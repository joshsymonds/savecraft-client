FROM ubuntu:24.04

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        curl \
        openssl \
        python3 \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash deck

USER deck
WORKDIR /home/deck

# Only the real install.sh and the test runner.
# Fixtures (keypair, dummy binaries, signatures) are generated at runtime.
COPY --chown=deck:deck install.sh /home/deck/fixtures/install.sh
COPY --chown=deck:deck test/run-test.sh /home/deck/run-test.sh
RUN chmod +x /home/deck/run-test.sh

CMD ["/home/deck/run-test.sh"]
