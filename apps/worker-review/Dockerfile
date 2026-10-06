FROM node:24.21.0-bookworm-slim AS dependencies
RUN npm install -g pnpm@12.8.1
WORKDIR /app
COPY . .
RUN pnpm install --frozen-lockfile
FROM node:24.21.0-bookworm-slim
RUN npm install -g pnpm@12.8.1
WORKDIR /app
COPY --from=dependencies --chown=node:node /app /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1
USER node
CMD ["pnpm","exec","turbo","run","worker:review","--filter=@reviewpilot/worker-review"]
