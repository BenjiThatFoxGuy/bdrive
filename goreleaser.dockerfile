# Alpine base with ffmpeg for video thumbnail generation.
# Previous scratch image couldn't run ffmpeg; alpine also provides ca-certificates
# natively, so the multi-stage cert copy is no longer needed.
FROM alpine:3.20
RUN apk add --no-cache ffmpeg ca-certificates
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/teldrive /teldrive
EXPOSE 8080
ENTRYPOINT ["/teldrive","run"]
