FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
# Registry is overridable for networks that cannot reach registry.npmjs.org:
#   --build-arg NPM_REGISTRY=https://your-mirror/
ARG NPM_REGISTRY=https://registry.npmjs.org
RUN --mount=type=cache,target=/root/.npm     npm config set registry "$NPM_REGISTRY" &&     npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM nginx:alpine
COPY nginx.conf /etc/nginx/nginx.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
