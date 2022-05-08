FROM node:latest as base

WORKDIR /app

# ---------- Builder ----------
# Creates:
# - node_modules: production dependencies (no dev dependencies)
# - dist: A production build compiled with Babel
FROM base AS builder

# COPY package*.json .env* .babelrc.json ./
COPY package*.json ./

RUN npm install

COPY ./src ./src
COPY ./public ./public

RUN npm run build

RUN npm prune --production # Remove dev dependencies
# ---------- Test -------------
FROM builder AS test

RUN npm run test

# ---------- Release ----------
FROM base AS release

COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/build ./dist
COPY ./entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENV NODE_ENV='production'

USER node

ENTRYPOINT ["/entrypoint.sh"]
#CMD ["node", "./dist/index.js"]

