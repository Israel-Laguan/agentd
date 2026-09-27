FROM python:3.11.12-slim-bookworm
WORKDIR /app
COPY server.py /app/server.py
RUN adduser --disabled-password --gecos '' --uid 10001 appuser && \
    chown -R appuser:appuser /app
USER appuser
EXPOSE 8000
CMD ["python", "/app/server.py"]
