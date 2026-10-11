package reviewcontext

import (
	"encoding/json"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"sync"
	"time"
)

type ephemeralContext struct {
	InputHash string
	Files     []github.PullFile
	Pages     []contracts.ReviewContextPage
	ExpiresAt time.Time
	Timer     *time.Timer
}

var memoryContexts = struct {
	sync.Mutex
	Entries map[string]ephemeralContext
}{Entries: map[string]ephemeralContext{}}

func EphemeralContext(lease contracts.ReviewLease) ([]github.PullFile, []contracts.ReviewContextPage, bool) {
	memoryContexts.Lock()
	defer memoryContexts.Unlock()
	entry, ok := memoryContexts.Entries[lease.RunId]
	if !ok || entry.InputHash != lease.InputHash || !entry.ExpiresAt.After(time.Now()) {
		return nil, nil, false
	}
	return entry.Files, entry.Pages, true
}
func keepEphemeral(lease contracts.ReviewLease, files []github.PullFile, pages []contracts.ReviewContextPage) error {
	body, _ := json.Marshal(struct {
		Files []github.PullFile
		Pages []contracts.ReviewContextPage
	}{files, pages})
	if len(body) > 8<<20 {
		return fault.New(409, "MEMORY_CONTEXT_CAPACITY")
	}
	memoryContexts.Lock()
	defer memoryContexts.Unlock()
	for id, entry := range memoryContexts.Entries {
		if !entry.ExpiresAt.After(time.Now()) {
			entry.Timer.Stop()
			delete(memoryContexts.Entries, id)
		}
	}
	prior, exists := memoryContexts.Entries[lease.RunId]
	if !exists && len(memoryContexts.Entries) >= 16 {
		return fault.New(409, "MEMORY_CONTEXT_CAPACITY")
	}
	if exists {
		prior.Timer.Stop()
	}
	expires := time.Now().Add(5 * time.Minute)
	if lease.Snapshot.Provenance.ContentExpiresAt.Before(expires) {
		expires = lease.Snapshot.Provenance.ContentExpiresAt
	}
	entry := ephemeralContext{InputHash: lease.InputHash, Files: files, Pages: pages, ExpiresAt: expires}
	// An already queued timer must not delete a refreshed entry.
	entry.Timer = time.AfterFunc(time.Until(expires), func() {
		memoryContexts.Lock()
		defer memoryContexts.Unlock()
		if current, ok := memoryContexts.Entries[lease.RunId]; ok && current.ExpiresAt.Equal(expires) {
			delete(memoryContexts.Entries, lease.RunId)
		}
	})
	memoryContexts.Entries[lease.RunId] = entry
	return nil
}
func ForgetEphemeral(runID string) {
	memoryContexts.Lock()
	defer memoryContexts.Unlock()
	if entry, ok := memoryContexts.Entries[runID]; ok {
		entry.Timer.Stop()
		delete(memoryContexts.Entries, runID)
	}
}
func metadataPages(pages []contracts.ReviewContextPage) []contracts.ReviewContextPage {
	body, _ := json.Marshal(pages)
	var result []contracts.ReviewContextPage
	_ = json.Unmarshal(body, &result)
	for i := range result {
		for j := range result[i].Files {
			f := &result[i].Files[j]
			f.Patch = nil
			f.Base.Text = nil
			f.Head.Text = nil
		}
		for j := range result[i].Evidence {
			result[i].Evidence[j].Snippet = ""
		}
	}
	return result
}
