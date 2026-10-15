# Disposable acceptance image. It is not the production release package.
FROM postgres:17
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/kura
COPY server/kura-server server/kura-account /usr/local/bin/
COPY scripts/library-backup.py scripts/online_backup_proof.py /opt/kura/scripts/
RUN mkdir -p /state/media /state/backups /state/replica /state/restore-media \
    && chown -R postgres:postgres /state
ENV HTTP_ADDR=0.0.0.0:8080 \
    MEDIA_ROOT=/state/media \
    KURA_BACKUP_ROOT=/state/backups \
    KURA_BACKUP_REPLICA_ROOT=/state/replica \
    KURA_ONLINE_BACKUP_SCRIPT=/opt/kura/scripts/online_backup_proof.py
USER postgres
ENTRYPOINT ["/usr/local/bin/kura-server"]
