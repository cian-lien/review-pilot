//go:build integration

package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/messaging"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/testsupport"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMessageRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store := testsupport.NewStore(t)
	var err error
	if err = persistence.ApplyMigrations(ctx, store, os.Getenv("MIGRATION_DIRECTORY")); err != nil {
		t.Fatal(err)
	}
	db := store.DB()
	for _, statement := range []string{
		"UPDATE runtime_control SET mode='go',epoch=1 WHERE id=1",
		"INSERT INTO installations(id) VALUES(101)",
		"INSERT INTO repositories(installation_id,id,full_name,public,enabled) VALUES(101,202,'acme/web',true,true)",
		"INSERT INTO pull_requests(installation_id,repository_id,number,base_sha,head_sha,remote_updated_at) VALUES(101,202,1,repeat('a',40),repeat('b',40),clock_timestamp())",
	} {
		if err = db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot := contracts.RunSnapshot{ModelConfigVersion: "none", RepositoryConfigVersion: "1", MemorySetVersion: "none", TrustedRulesSha: "none"}
	snapshot.Identity.Tenant = contracts.TenantRef{InstallationId: 101, RepositoryId: 202}
	snapshot.Identity.PrNumber = 1
	snapshot.Identity.BaseSha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	snapshot.Identity.HeadSha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	snapshot.Identity.PipelineVersion = "foundation-v1"
	payload, _ := json.Marshal(snapshot)
	runID := "00000000-0000-4000-8000-000000000001"
	taskID := "00000000-0000-4000-8000-000000000002"
	if err = db.Exec("INSERT INTO review_runs(id,installation_id,repository_id,pr_number,base_sha,head_sha,pipeline_version,generation,snapshot) VALUES(?,101,202,1,?,?,'foundation-v1',0,?::jsonb)", runID, snapshot.Identity.BaseSha, snapshot.Identity.HeadSha, string(payload)).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("INSERT INTO jobs(id,run_id) VALUES(?,?)", taskID, runID).Error; err != nil {
		t.Fatal(err)
	}
	envelope := contracts.TaskEnvelope{SchemaVersion: 1, MessageId: "00000000-0000-4000-8000-000000000003", TaskId: taskID, RunId: runID, HeadSha: snapshot.Identity.HeadSha, Tenant: snapshot.Identity.Tenant, Kind: contracts.TaskEnvelopeKindConnectionCheck}
	service := jobs.New(db)
	brokerURL, parseErr := url.Parse(os.Getenv("TEST_RABBITMQ_URL"))
	if parseErr != nil || brokerURL == nil || brokerURL.User == nil || brokerURL.Hostname() != "127.0.0.1" || brokerURL.User.Username() != "reviewpilot_test" {
		t.Fatal("Dedicated RabbitMQ required")
	}
	rabbit := messaging.NewRabbit(brokerURL.String())
	defer rabbit.Close()
	outboxBody, _ := json.Marshal(envelope)
	if err = db.Exec("INSERT INTO outbox_events(id,task_id,notification_generation,kind,payload) VALUES(?,?,0,'connection-check',?::jsonb)", envelope.MessageId, taskID, string(outboxBody)).Error; err != nil {
		t.Fatal(err)
	}
	lost := &loseConfirm{real: rabbit}
	relay := messaging.Relay{DB: db, Publisher: lost}
	if err = relay.Flush(ctx, 1); err == nil {
		t.Fatal("lost confirmation must leave outbox unsent")
	}
	var sent int64
	db.Raw("SELECT count(*) FROM outbox_events WHERE sent_at IS NOT NULL").Scan(&sent)
	if sent != 0 {
		t.Fatal("unconfirmed message marked sent")
	}
	db.Exec("UPDATE outbox_events SET available_at=clock_timestamp()")
	if err = relay.Flush(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err = rabbit.Publish(ctx, "unroutable-test", jobs.UUID(), []byte("{}")); err == nil {
		t.Fatal("mandatory basic.return must reject an otherwise positive confirm")
	}
	conn, err := amqp.Dial(brokerURL.String())
	if err != nil {
		t.Fatal("broker connect")
	}
	defer conn.Close()
	channel, err := conn.Channel()
	if err != nil {
		t.Fatal("broker channel")
	}
	first := get(t, channel)
	if err = channel.Close(); err != nil {
		t.Fatal(err)
	}
	channel, err = conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	redelivered := get(t, channel)
	if !redelivered.Redelivered {
		t.Fatal("closed unacknowledged channel must redeliver")
	}
	if string(redelivered.Body) != string(first.Body) {
		t.Fatal("redelivery changed notification")
	}
	duplicate := get(t, channel)
	if string(duplicate.Body) != string(first.Body) {
		t.Fatal("outbox retry changed identity")
	}
	var group sync.WaitGroup
	var mutex sync.Mutex
	var leases []contracts.TaskLease
	var busy int
	for _, worker := range []string{"test-worker-1", "test-worker-2"} {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			answer, err := service.Claim(ctx, envelope, id)
			if err != nil {
				t.Error(err)
				return
			}
			mutex.Lock()
			defer mutex.Unlock()
			if answer.Lease != nil {
				leases = append(leases, *answer.Lease)
			}
			if answer.Outcome == contracts.ClaimResponseOutcomeBusy {
				busy++
			}
		}(worker)
	}
	group.Wait()
	if len(leases) != 1 || busy != 1 {
		t.Fatal("two deliveries must acquire one fenced lease")
	}
	old := leases[0]
	oldResult := connection(old)
	if err = db.Exec("UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=?", taskID).Error; err != nil {
		t.Fatal(err)
	}
	if status(service.Heartbeat(ctx, old)) != 409 {
		t.Fatal("expired heartbeat accepted")
	}
	_, err = service.CommitConnection(ctx, oldResult)
	oldStatus := status(err)
	if oldStatus != 409 {
		t.Fatal("old lease accepted")
	}
	if err = service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	var notification struct{ Payload json.RawMessage }
	if err = db.Raw("SELECT payload FROM outbox_events WHERE task_id=? ORDER BY notification_generation DESC LIMIT 1", taskID).Scan(&notification).Error; err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(notification.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	claim, err := service.Claim(ctx, envelope, "recovered-worker")
	if err != nil || claim.Lease == nil {
		t.Fatal("recovery did not create claimable notification", err)
	}
	lease := *claim.Lease
	db.Exec("UPDATE runtime_control SET epoch=2 WHERE id=1")
	if status(service.Heartbeat(ctx, lease)) != 409 {
		t.Fatal("old epoch renewed")
	}
	db.Exec("UPDATE runtime_control SET epoch=1 WHERE id=1")
	result := connection(lease)
	router := httpapi.NewRouter()
	cfg := config.Config{ReviewWorkerServiceToken: strings.Repeat("r", 40), EvolutionWorkerServiceToken: strings.Repeat("e", 40)}
	jobs.Register(router, service, cfg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "connection-result") {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, r)
			if recorder.Code != 200 {
				t.Error("commit did not succeed:", recorder.Code)
			}
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				connection.Close()
			}
			return
		}
		router.ServeHTTP(w, r)
	}))
	defer server.Close()
	body, _ := json.Marshal(result)
	request, _ := http.NewRequest("POST", server.URL+"/internal/tasks/"+taskID+"/connection-result", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+cfg.ReviewWorkerServiceToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err == nil {
		response.Body.Close()
		t.Fatal("commit response loss not simulated")
	}
	query := url.Values{"runId": {runID}, "inputHash": {result.InputHash}, "resultHash": {result.ResultHash}}
	readRequest, _ := http.NewRequest("GET", server.URL+"/internal/tasks/"+taskID+"/result?"+query.Encode(), nil)
	readRequest.Header.Set("Authorization", "Bearer "+cfg.ReviewWorkerServiceToken)
	response, err = server.Client().Do(readRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var accepted contracts.CommitResponse
	if err = json.NewDecoder(response.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	recovered := response.StatusCode == 200 && accepted.Outcome == contracts.AlreadyAccepted
	if !recovered {
		t.Fatal("matching read must recover lost response")
	}
	if err = channel.Ack(redelivered.DeliveryTag, false); err != nil {
		t.Fatal(err)
	}
	if err = channel.Ack(duplicate.DeliveryTag, false); err != nil {
		t.Fatal(err)
	}
	confirmation, err := service.CommitConnection(ctx, oldResult)
	if err != nil || confirmation.Outcome != contracts.AlreadyAccepted {
		t.Fatal("identical accepted result must survive expired lease")
	}
	result.ResultHash = strings.Repeat("f", 64)
	_, err = service.CommitConnection(ctx, result)
	changedStatus := status(err)
	if changedStatus != 409 {
		t.Fatal("changed result silently accepted")
	}
	roleRequest := httptest.NewRequest("GET", "/internal/tasks/"+taskID+"/result?"+query.Encode(), nil)
	roleRequest.Header.Set("Authorization", "Bearer "+cfg.EvolutionWorkerServiceToken)
	roleResponse := httptest.NewRecorder()
	router.ServeHTTP(roleResponse, roleRequest)
	if roleResponse.Code != 403 {
		t.Fatal("cross-role read allowed")
	}
	var count, publications int64
	db.Raw("SELECT count(*) FROM job_results WHERE task_id=?", taskID).Scan(&count)
	db.Raw("SELECT count(*) FROM publication_requests WHERE run_id=?", runID).Scan(&publications)
	if count != 1 || publications != 1 {
		t.Fatal("result and publication intent must be atomic and unique")
	}
	observed := map[string]any{"activeExecutions": len(leases), "acceptedResultRows": count, "oldLeaseStatus": oldStatus, "changedResultStatus": changedStatus, "recoveredByRead": recovered, "acknowledgedBeforeCommit": false, "brokerRedelivered": redelivered.Redelivered}
	encoded, _ := json.Marshal(observed)
	if err = os.WriteFile(os.Getenv("SCENARIO_RESULT_PATH"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
func connection(l contracts.TaskLease) contracts.ConnectionResult {
	return contracts.ConnectionResult{TaskId: l.TaskId, RunId: l.RunId, LeaseToken: l.LeaseToken, ExecutionEpoch: l.ExecutionEpoch, InputHash: l.InputHash, ResultHash: jobs.ResultHash(l.TaskId, l.RunId, l.InputHash), ResultId: l.TaskId + ":connection", Kind: contracts.ConnectionResultKindConnectionCheck}
}
func status(err error) int {
	var failure *fault.Error
	if errors.As(err, &failure) {
		return failure.Status
	}
	if err != nil {
		return 500
	}
	return 200
}

type loseConfirm struct {
	real *messaging.Rabbit
	lost bool
}

func (p *loseConfirm) Publish(ctx context.Context, key, id string, body []byte) error {
	if err := p.real.Publish(ctx, key, id, body); err != nil {
		return err
	}
	if !p.lost {
		p.lost = true
		return errors.New("CONFIRM_LOST")
	}
	return nil
}
func get(t *testing.T, channel *amqp.Channel) amqp.Delivery {
	t.Helper()
	for attempt := 0; attempt < 40; attempt++ {
		delivery, ok, err := channel.Get(messaging.ReviewQueue, false)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			return delivery
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("real broker delivery missing")
	return amqp.Delivery{}
}
