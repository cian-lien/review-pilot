FROM node:24.21.0-bookworm-slim AS dependencies
RUN npm install -g pnpm@12.8.1
WORKDIR /app
COPY . .
RUN pnpm install --frozen-lockfile
FROM node:24.21.0-bookworm-slim
RUN npm install -g pnpm@12.8.1
WORKDIR /app
# Runtime source exports require these workspace links, not the API or Web source trees.
COPY --from=dependencies --chown=node:node /app/node_modules /app/node_modules
COPY --from=dependencies --chown=node:node /app/package.json /app/pnpm-workspace.yaml /app/pnpm-lock.yaml /app/turbo.json /app/tsconfig.json /app/tsconfig.base.json /app/
COPY --from=dependencies --chown=node:node /app/apps/worker-review /app/apps/worker-review
COPY --from=dependencies --chown=node:node /app/packages/worker-runtime /app/packages/worker-runtime
COPY --from=dependencies --chown=node:node /app/packages/contracts /app/packages/contracts
COPY --from=dependencies --chown=node:node /app/packages/observability /app/packages/observability
COPY --from=dependencies --chown=node:node /app/packages/repository /app/packages/repository
COPY --from=dependencies --chown=node:node /app/packages/model-gateway /app/packages/model-gateway
COPY --from=dependencies --chown=node:node /app/packages/review-engine /app/packages/review-engine
COPY --from=dependencies --chown=node:node /app/scripts/container-health.mjs /app/scripts/container-health.mjs
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1
USER node
CMD ["pnpm","exec","turbo","run","worker:review","--filter=@reviewpilot/worker-review"]
