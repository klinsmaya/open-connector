FROM scratch
COPY gateway /gateway
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT ["/gateway"]
