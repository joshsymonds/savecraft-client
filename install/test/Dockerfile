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

COPY --chown=deck:deck fixtures/ /home/deck/fixtures/
COPY --chown=deck:deck run-test.sh /home/deck/run-test.sh
RUN chmod +x /home/deck/run-test.sh

CMD ["/home/deck/run-test.sh"]
