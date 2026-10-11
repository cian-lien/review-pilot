export interface paths {
    "/api/health": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getHealth"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/bootstrap": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getBootstrap"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/session": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getSession"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/repositories": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listRepositories"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/runs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["listRuns"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/repositories/sync": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["syncRepositories"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/repositories/{installationId}/{repositoryId}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                installationId: number;
                repositoryId: number;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["setRepositoryEnabled"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/tasks/{taskId}/claim": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["claimTask"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/tasks/{taskId}/heartbeat": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["heartbeatTask"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/tasks/{taskId}/connection-result": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["commitConnectionResult"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/tasks/{taskId}/result": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["readConnectionResult"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/tasks/{taskId}/fail": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["failTask"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/github/webhook": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description Verify SHA-256 HMAC over exact raw bytes before parsing; maximum 2 MiB. */
        post: operations["acceptGitHubWebhook"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/claim": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewClaimReviewRequestclaim"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/heartbeat": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewReviewLeaseheartbeat"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/context": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewReadReviewContextRequestcontext"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/model-calls/reserve": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewReserveModelCallRequestmodelcallsreserve"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/model-calls/{callId}/dispatch": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewDispatchModelCallRequestmodelcallscallIddispatch"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/model-calls/{callId}/settle": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewSettleModelCallRequestmodelcallscallIdsettle"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/stages/read": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewReadReviewStageRequeststagesread"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/stages": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewSaveReviewStageRequeststages"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/result": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["confirmReviewResult"];
        put?: never;
        post: operations["reviewCommitReviewRequestresult"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/block": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewReviewTaskActionRequestblock"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/internal/review-tasks/{taskId}/fail": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post: operations["reviewReviewTaskActionRequestfail"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/repositories/{installationId}/{repositoryId}/pulls": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getReviewPullList"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/repositories/{installationId}/{repositoryId}/pulls/{number}/runs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getReviewRunList"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/repositories/{installationId}/{repositoryId}/pulls/{number}/runs/{runId}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get: operations["getReviewRunDetail"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        TenantRef: {
            installationId: number;
            repositoryId: number;
        };
        RunSnapshot: {
            identity: {
                tenant: components["schemas"]["TenantRef"];
                prNumber: number;
                baseSha: string;
                headSha: string;
                pipelineVersion: string;
                generation: number;
            };
            modelConfigVersion: string;
            repositoryConfigVersion: string;
            memorySetVersion: string;
            trustedRulesSha: string;
        };
        TaskEnvelope: {
            /** @enum {integer} */
            schemaVersion: 1;
            messageId: string;
            taskId: string;
            runId: string;
            tenant: components["schemas"]["TenantRef"];
            headSha: string;
            /** @enum {string} */
            kind: "connection-check" | "evolution";
            notificationGeneration: number;
            traceparent?: string;
            tracestate?: string;
        };
        TaskLease: {
            taskId: string;
            runId: string;
            leaseToken: string;
            attempt: number;
            /** Format: date-time */
            expiresAt: string;
            snapshot: components["schemas"]["RunSnapshot"];
            inputHash: string;
            executionEpoch: number;
        };
        ClaimRequest: {
            workerId: string;
            envelope: components["schemas"]["TaskEnvelope"];
        };
        ClaimResponse: {
            /** @enum {string} */
            outcome: "claimed" | "busy" | "not-ready" | "finished" | "cancelled" | "exhausted" | "superseded";
            lease?: components["schemas"]["TaskLease"];
        };
        ConnectionResult: {
            resultId: string;
            taskId: string;
            runId: string;
            leaseToken: string;
            executionEpoch: number;
            inputHash: string;
            resultHash: string;
            /** @enum {string} */
            kind: "connection-check";
        };
        CommitResponse: {
            /** @enum {string} */
            outcome: "accepted" | "already-accepted";
        };
        APIError: {
            code: string;
            message: string;
            requestId: string;
        };
        HealthResponse: {
            /** @enum {string} */
            phase: "foundation";
            /** @enum {boolean} */
            aiReviewEnabled: false;
        };
        BootstrapResponse: {
            /** @enum {string} */
            phase: "foundation";
            /** @enum {boolean} */
            aiReviewEnabled: false;
            missingConfiguration: string[];
            installUrl?: string;
        };
        SessionResponse: {
            user: {
                id: number;
                login: string;
            };
            csrfToken: string;
        };
        RepositorySummary: {
            tenant: components["schemas"]["TenantRef"];
            fullName: string;
            public: boolean;
            enabled: boolean;
            configVersion: number;
            canManage: boolean;
        };
        RepositoryListResponse: {
            repositories: components["schemas"]["RepositorySummary"][];
        };
        RunSummary: {
            id: string;
            tenant: components["schemas"]["TenantRef"];
            fullName: string;
            prNumber: number;
            headSha: string;
            /** @enum {string} */
            status: "Queued" | "Indexing" | "Reviewing" | "Verifying" | "Publishing" | "Completed" | "Partial" | "Stale" | "Cancelled" | "Failed";
            /** Format: date-time */
            createdAt: string;
            checkId?: number;
        };
        RunListResponse: {
            runs: components["schemas"]["RunSummary"][];
            /** @enum {boolean} */
            aiReviewEnabled: false;
        };
        SetRepositoryEnabledRequest: {
            enabled: boolean;
            configVersion: number;
        };
        WebhookResponse: {
            accepted: boolean;
        };
        ReviewTaskEnvelope: {
            /** @enum {integer} */
            schemaVersion: 2;
            messageId: string;
            taskId: string;
            runId: string;
            tenant: components["schemas"]["TenantRef"];
            headSha: string;
            /** @enum {string} */
            kind: "ai-review";
            notificationGeneration: number;
            traceparent?: string;
            tracestate?: string;
        };
        /** @enum {string} */
        RunState: "Queued" | "Indexing" | "Reviewing" | "Verifying" | "Publishing" | "Completed" | "Partial" | "Stale" | "Cancelled" | "Failed";
        ReviewPolicy: {
            maxChangedLines: number;
            maxFiles: number;
            maxFileBytes: number;
            maxRawBytes: number;
            maxPageBytes: number;
            maxStageBytes: number;
            maxResultBytes: number;
            maxCandidates: number;
            maxReferences: number;
            maxSnippetBytes: number;
            maxInputTokens: number;
            maxOutputTokens: number;
            timeoutSeconds: number;
            retainContent: boolean;
        };
        ReviewIdentity: {
            tenant: components["schemas"]["TenantRef"];
            prNumber: number;
            baseSha: string;
            headSha: string;
            /** @enum {string} */
            pipelineVersion: "review-v1";
            generation: number;
            repositoryFullName: string;
            prTitle: string;
        };
        ReviewProvenance: {
            /** @enum {string} */
            modelMode: "fake" | "real";
            /** @enum {string} */
            githubMode: "fake" | "real";
            fixtureId: string;
            /** Format: date-time */
            contentExpiresAt: string;
        };
        ReviewSnapshot: {
            identity: components["schemas"]["ReviewIdentity"];
            modelConfigVersion: string;
            priceVersion: string;
            repositoryConfigVersion: number;
            /** @enum {string} */
            generationPromptVersion: "review-generation-v1";
            /** @enum {string} */
            verificationPromptVersion: "review-verification-v1";
            /** @enum {string} */
            memorySetVersion: "none";
            /** @enum {string} */
            trustedRulesSha: "none";
            policy: components["schemas"]["ReviewPolicy"];
            provenance: components["schemas"]["ReviewProvenance"];
        };
        ReviewLease: {
            taskId: string;
            runId: string;
            tenant: components["schemas"]["TenantRef"];
            headSha: string;
            inputHash: string;
            leaseToken: string;
            /** Format: date-time */
            leaseExpiresAt: string;
            executionEpoch: number;
            attempt: number;
            snapshot: components["schemas"]["ReviewSnapshot"];
        };
        ReviewSkippedFile: {
            path: string;
            reasonCode: string;
            changedLines: number;
        };
        ReviewCoverage: {
            complete: boolean;
            totalFiles: number;
            readFiles: number;
            totalChangedLines: number;
            coveredChangedLines: number;
            rawBytes: number;
            skipped: components["schemas"]["ReviewSkippedFile"][];
            limitations: string[];
        };
        ReviewFileContent: {
            /** @enum {string} */
            state: "available" | "deleted" | "binary" | "missing" | "denied" | "too-large";
            sha: string;
            contentHash: string | null;
            text: string | null;
            bytes: number;
        };
        ReviewChangedFile: {
            path: string;
            previousPath: string | null;
            /** @enum {string} */
            status: "added" | "modified" | "removed" | "renamed";
            additions: number;
            deletions: number;
            patch: string | null;
            base: components["schemas"]["ReviewFileContent"];
            head: components["schemas"]["ReviewFileContent"];
        };
        EvidenceRef: {
            evidenceId: string;
            path: string;
            sha: string;
            startLine: number;
            endLine: number;
            contentHash: string;
        };
        CodeEvidence: {
            ref: components["schemas"]["EvidenceRef"];
            tenant: components["schemas"]["TenantRef"];
            snippet: string;
            /** @enum {string} */
            readState: "available";
            redacted: boolean;
        };
        ReviewContextPage: {
            manifestId: string;
            contextHash: string | null;
            cursor: string | null;
            nextCursor: string | null;
            complete: boolean;
            files: components["schemas"]["ReviewChangedFile"][];
            evidence: components["schemas"]["CodeEvidence"][];
            coverage: components["schemas"]["ReviewCoverage"];
        };
        RootCauseStatement: {
            trigger: string;
            consequence: string;
        };
        ReviewCandidate: {
            candidateId: string;
            title: string;
            /** @enum {string} */
            severity: "high" | "medium" | "low";
            /** @enum {string} */
            category: "concurrency" | "validation" | "authorization" | "lifecycle" | "error-handling" | "data-integrity" | "other";
            failureScenario: string;
            impact: string;
            introducedByPR: boolean;
            evidence: components["schemas"]["EvidenceRef"][];
            suggestedVerification: string;
            rootCauseStatement: components["schemas"]["RootCauseStatement"];
        };
        ReviewCandidates: {
            candidates: components["schemas"]["ReviewCandidate"][];
        };
        VerificationDecision: {
            candidateId: string;
            /** @enum {string} */
            verdict: "verified" | "abstain";
            reasonCode: string;
            evidence: components["schemas"]["EvidenceRef"][];
        };
        ReviewVerification: {
            decisions: components["schemas"]["VerificationDecision"][];
        };
        ModelUsage: {
            inputTokens: number;
            outputTokens: number;
        };
        ModelCall: {
            callId: string;
            reservationId: string;
            /** @enum {string} */
            purpose: "generate" | "verify";
            ordinal: number;
            requestHash: string;
            /** @enum {string} */
            state: "reserved" | "dispatching" | "settled" | "released" | "uncertain";
            priceVersion: string;
            currency: string;
            reservedCostMicros: string;
            actualCostMicros: string | null;
            usage: components["schemas"]["ModelUsage"] | null;
            revision: number;
            sendAuthorized: boolean;
            reasonCode: string | null;
        };
        ReviewStagePayload: {
            context: components["schemas"]["ReviewContextPage"] | null;
            candidates: components["schemas"]["ReviewCandidates"] | null;
            verification: components["schemas"]["ReviewVerification"] | null;
        };
        ReviewStageRecord: {
            /** @enum {string} */
            stage: "context" | "candidates" | "verification";
            stageInputHash: string;
            outputHash: string;
            callId: string | null;
            payload: components["schemas"]["ReviewStagePayload"];
            /** Format: date-time */
            expiresAt: string;
        };
        ReviewResult: {
            /** @enum {string} */
            kind: "ai-review";
            lease: components["schemas"]["ReviewLease"];
            resultId: string;
            contextHash: string;
            candidates: components["schemas"]["ReviewCandidate"][];
            decisions: components["schemas"]["VerificationDecision"][];
            coverage: components["schemas"]["ReviewCoverage"];
            callIds: string[];
        };
        ReviewFinding: {
            findingId: string;
            fingerprint: string;
            candidate: components["schemas"]["ReviewCandidate"];
            evidence: components["schemas"]["CodeEvidence"][];
            /** @enum {string} */
            publicationStatus: "pending" | "published" | "not-published" | "uncertain";
            reasonCode: string | null;
            contentExpired: boolean;
        };
        ReviewFilteredCandidate: {
            candidateId: string;
            title: string;
            reasonCode: string;
            evidence: components["schemas"]["EvidenceRef"][];
        };
        ReviewCostSummary: {
            currency: string;
            priceVersion: string;
            calls: components["schemas"]["ModelCall"][];
            knownCostMicros: string;
            heldCostMicros: string;
            needsReconciliation: boolean;
            monthlyWarning: boolean;
            /** Format: date-time */
            dailyResetsAt: string;
        };
        ReviewPublicationFinding: {
            findingId: string;
            fingerprint: string;
            path: string;
            line: number | null;
            body: string;
        };
        ReviewPublicationPlan: {
            runId: string;
            tenant: components["schemas"]["TenantRef"];
            prNumber: number;
            headSha: string;
            planHash: string;
            marker: string;
            findings: components["schemas"]["ReviewPublicationFinding"][];
            /** @enum {string} */
            mode: "fake" | "real";
        };
        ReviewPublicationReceipt: {
            reviewId: string | null;
            commentIds: string[];
            checkId: string | null;
            headSha: string;
            authorAppId: number;
            marker: string;
            /** @enum {string} */
            state: "confirmed" | "uncertain" | "not-written";
            /** @enum {string} */
            mode: "fake" | "real";
        };
        ReviewPublicationSummary: {
            /** @enum {string} */
            state: "pending" | "published" | "cancelled" | "failed" | "uncertain";
            occupiedSlots: number;
            receipt: components["schemas"]["ReviewPublicationReceipt"] | null;
        };
        ReviewRunSummary: {
            runId: string;
            headSha: string;
            status: components["schemas"]["RunState"];
            needsReconciliation: boolean;
            reasonCode: string | null;
            /** Format: date-time */
            createdAt: string;
            /** @enum {string} */
            mode: "fake" | "real";
        };
        ReviewRunDetail: {
            runId: string;
            status: components["schemas"]["RunState"];
            needsReconciliation: boolean;
            reasonCode: string | null;
            snapshot: components["schemas"]["ReviewSnapshot"];
            findings: components["schemas"]["ReviewFinding"][];
            filtered: components["schemas"]["ReviewFilteredCandidate"][];
            coverage: components["schemas"]["ReviewCoverage"];
            costs: components["schemas"]["ReviewCostSummary"];
            publication: components["schemas"]["ReviewPublicationSummary"];
            contentExpired: boolean;
        };
        ReviewPullSummary: {
            prNumber: number;
            title: string;
            headSha: string;
            latestRun: components["schemas"]["ReviewRunSummary"];
        };
        ReviewPullList: {
            items: components["schemas"]["ReviewPullSummary"][];
            nextCursor: string | null;
        };
        ReviewRunList: {
            items: components["schemas"]["ReviewRunSummary"][];
            nextCursor: string | null;
        };
        ClaimReviewRequest: {
            envelope: components["schemas"]["ReviewTaskEnvelope"];
            workerId: string;
        };
        ClaimReviewResponse: {
            /** @enum {string} */
            outcome: "claimed" | "busy" | "accepted" | "cancelled" | "terminal" | "blocked";
            lease: components["schemas"]["ReviewLease"] | null;
        };
        ReadReviewContextRequest: {
            lease: components["schemas"]["ReviewLease"];
            cursor: string | null;
        };
        ReserveModelCallRequest: {
            modelConfigHash: string;
            lease: components["schemas"]["ReviewLease"];
            /** @enum {string} */
            purpose: "generate" | "verify";
            ordinal: number;
            requestHash: string;
            inputTokenLimit: number;
            outputTokenLimit: number;
        };
        DispatchModelCallRequest: {
            lease: components["schemas"]["ReviewLease"];
            callId: string;
            expectedRevision: number;
        };
        SettleModelCallRequest: {
            lease: components["schemas"]["ReviewLease"];
            callId: string;
            expectedRevision: number;
            /** @enum {string} */
            outcome: "succeeded" | "failed" | "not-sent" | "uncertain";
            usage: components["schemas"]["ModelUsage"] | null;
            safeResponseHash: string | null;
            reasonCode: string | null;
        };
        ReadReviewStageRequest: {
            lease: components["schemas"]["ReviewLease"];
            /** @enum {string} */
            stage: "context" | "candidates" | "verification";
            stageInputHash: string;
        };
        ReadReviewStageResponse: {
            found: boolean;
            record: components["schemas"]["ReviewStageRecord"] | null;
        };
        SaveReviewStageRequest: {
            lease: components["schemas"]["ReviewLease"];
            /** @enum {string} */
            stage: "context" | "candidates" | "verification";
            stageInputHash: string;
            callId: string | null;
            payload: components["schemas"]["ReviewStagePayload"];
        };
        CommitReviewRequest: {
            result: components["schemas"]["ReviewResult"];
        };
        ReviewCommitResponse: {
            /** @enum {string} */
            outcome: "accepted" | "already-accepted";
            resultId: string;
            resultHash: string;
        };
        ReviewTaskActionRequest: {
            lease: components["schemas"]["ReviewLease"];
            reasonCode: string;
        };
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getHealth: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HealthResponse"];
                };
            };
        };
    };
    getBootstrap: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BootstrapResponse"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    getSession: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionResponse"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    listRepositories: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RepositoryListResponse"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    listRuns: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RunListResponse"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    syncRepositories: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Synchronized */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    setRepositoryEnabled: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                installationId: number;
                repositoryId: number;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SetRepositoryEnabledRequest"];
            };
        };
        responses: {
            /** @description Updated */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    claimTask: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ClaimRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ClaimResponse"];
                };
            };
            /** @description Success */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    heartbeatTask: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["TaskLease"];
            };
        };
        responses: {
            /** @description Success */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Success */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    commitConnectionResult: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ConnectionResult"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CommitResponse"];
                };
            };
            /** @description Success */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    readConnectionResult: {
        parameters: {
            query: {
                runId: string;
                inputHash: string;
                resultHash: string;
            };
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CommitResponse"];
                };
            };
            /** @description Success */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    failTask: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["TaskLease"];
            };
        };
        responses: {
            /** @description Success */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Success */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    acceptGitHubWebhook: {
        parameters: {
            query?: never;
            header: {
                "X-GitHub-Delivery": string;
                "X-GitHub-Event": string;
                "X-Hub-Signature-256": string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": {
                    [key: string]: unknown;
                };
            };
        };
        responses: {
            /** @description Response */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WebhookResponse"];
                };
            };
            /** @description Response */
            400: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Response */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Response */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Response */
            413: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Response */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewClaimReviewRequestclaim: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ClaimReviewRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ClaimReviewResponse"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewReviewLeaseheartbeat: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReviewLease"];
            };
        };
        responses: {
            /** @description Success */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewReadReviewContextRequestcontext: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReadReviewContextRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReviewContextPage"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewReserveModelCallRequestmodelcallsreserve: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReserveModelCallRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModelCall"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewDispatchModelCallRequestmodelcallscallIddispatch: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
                callId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DispatchModelCallRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModelCall"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewSettleModelCallRequestmodelcallscallIdsettle: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
                callId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SettleModelCallRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModelCall"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewReadReviewStageRequeststagesread: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReadReviewStageRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReadReviewStageResponse"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewSaveReviewStageRequeststages: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SaveReviewStageRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CommitResponse"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    confirmReviewResult: {
        parameters: {
            query?: {
                runId?: string;
                inputHash?: string;
            };
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReviewCommitResponse"];
                };
            };
            /** @description Success */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewCommitReviewRequestresult: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CommitReviewRequest"];
            };
        };
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReviewCommitResponse"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewReviewTaskActionRequestblock: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReviewTaskActionRequest"];
            };
        };
        responses: {
            /** @description Success */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    reviewReviewTaskActionRequestfail: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                taskId: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReviewTaskActionRequest"];
            };
        };
        responses: {
            /** @description Success */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
            /** @description Success */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    getReviewPullList: {
        parameters: {
            query?: {
                cursor?: string;
            };
            header?: never;
            path: {
                installationId: number;
                repositoryId: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReviewPullList"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    getReviewRunList: {
        parameters: {
            query?: {
                cursor?: string;
            };
            header?: never;
            path: {
                installationId: number;
                repositoryId: number;
                number: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReviewRunList"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
    getReviewRunDetail: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                installationId: number;
                repositoryId: number;
                number: number;
                runId: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Success */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReviewRunDetail"];
                };
            };
            /** @description Success */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["APIError"];
                };
            };
        };
    };
}
