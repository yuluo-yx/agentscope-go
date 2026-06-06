FROM python:3.11-slim

WORKDIR /home/user

COPY runtime.py /app/runtime.py

RUN chown -R 1000:1000 /home/user /app

USER 1000

EXPOSE 8888

CMD ["python", "/app/runtime.py"]
