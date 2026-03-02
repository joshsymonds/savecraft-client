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

# Build context is install/ — copy the real install.sh from the root,
# then the test fixtures and runner from test/.
COPY --chown=deck:deck install.sh /home/deck/fixtures/install.sh
COPY --chown=deck:deck test/fixtures/ /home/deck/fixtures/
COPY --chown=deck:deck test/run-test.sh /home/deck/run-test.sh
RUN chmod +x /home/deck/run-test.sh

CMD ["/home/deck/run-test.sh"]
