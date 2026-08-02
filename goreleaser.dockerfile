# scratch has no CA bundle at all, so any outbound TLS connection needing to
# verify a certificate against a public CA fails with "x509: certificate
# signed by unknown authority" - regardless of whether the remote cert is
# valid. This stage exists purely to source one; buildx resolves it for
# whichever platform is being built, same as the final COPY below.
FROM alpine:3.20 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
ARG TARGETPLATFORM
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY $TARGETPLATFORM/teldrive /teldrive
EXPOSE 8080
ENTRYPOINT ["/teldrive","run"]
