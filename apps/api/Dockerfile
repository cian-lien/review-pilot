FROM golang:1.27.1-bookworm AS go-toolchain
FROM node:24.21.0-bookworm-slim AS dependencies
RUN npm install -g pnpm@12.8.1
WORKDIR /app
COPY . .
RUN pnpm install --frozen-lockfile

COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"
RUN pnpm exec turbo run build --filter=@reviewpilot/api
FROM node:24.21.0-bookworm-slim
RUN npm install -g pnpm@12.8.1
WORKDIR /app
COPY --from=dependencies --chown=node:node /app /app
ENV NODE_ENV=production
USER node
CMD ["pnpm","exec","turbo","run","start","--filter=@reviewpilot/api"]
