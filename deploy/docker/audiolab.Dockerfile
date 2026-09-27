# syntax=docker/dockerfile:1

FROM python:3.12-slim
# ffmpeg is used to decode audio from downloaded pieces.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --no-create-home marquee
WORKDIR /app
COPY py/audiolab/ ./
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1
USER marquee
EXPOSE 7710
CMD ["python", "-m", "marquee_audiolab"]
