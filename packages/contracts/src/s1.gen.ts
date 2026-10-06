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
}
