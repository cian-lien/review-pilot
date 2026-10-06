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
}
