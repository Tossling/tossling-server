FROM python:3.13-slim
RUN pip install --no-cache-dir cryptography==44.0.0
WORKDIR /app
COPY bot.py sample.png ./
ENV PYTHONUNBUFFERED=1
CMD ["python", "bot.py"]
