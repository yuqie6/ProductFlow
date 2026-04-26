FROM node:22-alpine AS build

WORKDIR /app/web

COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable \
    && corepack prepare pnpm@10.25.0 --activate \
    && pnpm install --frozen-lockfile

COPY web/ ./
ARG VITE_API_BASE_URL=
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
RUN pnpm build

FROM nginx:1.27-alpine AS runtime

COPY web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/web/dist /usr/share/nginx/html

EXPOSE 80
