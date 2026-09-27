# syntax=docker/dockerfile:1

FROM python:3.12-slim
RUN useradd --system --uid 10001 --no-create-home marquee
WORKDIR /app
COPY py/recs/ ./
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1
USER marquee
EXPOSE 7720
CMD ["python", "-m", "marquee_recs"]
