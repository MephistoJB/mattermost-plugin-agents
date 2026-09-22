import json
import os
import subprocess
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.responses import Response
from pydantic import BaseModel


MIMIR_TRANSCRIBE_URL = os.environ.get("MIMIR_TRANSCRIBE_URL", "http://172.17.0.1:8091/transcribe")
MIMIR_API_HEADER = os.environ.get("MIMIR_API_HEADER", "Authorization")
MIMIR_API_TOKEN = os.environ.get("MIMIR_API_TOKEN", "")
TTS_VOICE = os.environ.get("VOICE_ADAPTER_TTS_VOICE", "slt")
DEFAULT_FORMAT = os.environ.get("VOICE_ADAPTER_OUTPUT_FORMAT", "mp3")
MAX_TTS_CHARS = int(os.environ.get("VOICE_ADAPTER_MAX_TTS_CHARS", "4000"))

app = FastAPI(title="Mattermost Agents Local Voice Adapter", docs_url=None, redoc_url=None)


class SpeechRequest(BaseModel):
    model: str | None = None
    voice: str | None = None
    input: str
    response_format: str | None = None


@app.get("/health")
def health() -> dict:
    return {
        "status": "ok",
        "stt": MIMIR_TRANSCRIBE_URL,
        "tts": "ffmpeg-flite",
    }


@app.post("/v1/audio/transcriptions")
async def transcriptions(
    file: UploadFile = File(...),
    model: str = Form("whisper-1"),
    response_format: str = Form("json"),
) -> dict:
    del model
    content = await file.read()
    if not content:
        raise HTTPException(status_code=400, detail="empty audio upload")

    boundary = "mattermostagentsvoiceadapter"
    filename = (file.filename or "audio.bin").replace('"', "")
    body = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="data"; filename="{filename}"\r\n'
        f"Content-Type: {file.content_type or 'application/octet-stream'}\r\n\r\n"
    ).encode("utf-8") + content + f"\r\n--{boundary}--\r\n".encode("utf-8")

    request = urllib.request.Request(MIMIR_TRANSCRIBE_URL, data=body, method="POST")
    request.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
    if MIMIR_API_TOKEN:
        request.add_header(MIMIR_API_HEADER, MIMIR_API_TOKEN)

    try:
        with urllib.request.urlopen(request, timeout=900) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", errors="replace")[-2000:]
        raise HTTPException(status_code=error.code, detail=detail) from error
    except Exception as error:
        raise HTTPException(status_code=502, detail=f"local transcription adapter failed: {error}") from error

    text = (payload.get("raw_txt") or "").strip()
    if not text and isinstance(payload.get("segments"), list):
        text = " ".join(str(segment.get("text", "")).strip() for segment in payload["segments"]).strip()
    if (response_format or "").lower() == "vtt":
        return Response(content=to_vtt(payload, text), media_type="text/vtt")
    return {
        "text": text,
        "segments": payload.get("segments", []),
        "language": "auto",
    }


def to_vtt(payload: dict, fallback_text: str) -> str:
    segments = payload.get("segments")
    if isinstance(segments, list) and segments:
        lines = ["WEBVTT", ""]
        for segment in segments:
            start = str(segment.get("start", "00:00:00,000")).replace(",", ".")
            end = str(segment.get("end", "00:00:00,000")).replace(",", ".")
            text = str(segment.get("text", "")).strip()
            if text:
                lines.extend([f"{start} --> {end}", text, ""])
        return "\n".join(lines).strip() + "\n"

    text = (fallback_text or "").strip()
    if not text:
        text = " "
    return f"WEBVTT\n\n00:00:00.000 --> 00:00:05.000\n{text}\n"


@app.post("/v1/audio/speech")
def speech(request: SpeechRequest) -> Response:
    text = (request.input or "").strip()
    if not text:
        raise HTTPException(status_code=400, detail="input is required")
    if len(text) > MAX_TTS_CHARS:
        text = text[:MAX_TTS_CHARS]

    response_format = (request.response_format or DEFAULT_FORMAT or "mp3").lower()
    if response_format not in {"mp3", "wav"}:
        response_format = "mp3"
    voice = (request.voice or TTS_VOICE or "slt").strip()

    with tempfile.TemporaryDirectory() as tmpdir:
        text_path = Path(tmpdir) / "input.txt"
        out_path = Path(tmpdir) / f"speech.{response_format}"
        text_path.write_text(text, encoding="utf-8")

        codec_args = ["-codec:a", "libmp3lame", "-q:a", "4"] if response_format == "mp3" else ["-codec:a", "pcm_s16le"]
        command = [
            "ffmpeg",
            "-hide_banner",
            "-loglevel",
            "error",
            "-y",
            "-f",
            "lavfi",
            "-i",
            f"flite=textfile={text_path}:voice={voice}",
            *codec_args,
            str(out_path),
        ]
        try:
            subprocess.run(command, check=True, capture_output=True, text=True, timeout=120)
        except subprocess.CalledProcessError as error:
            detail = (error.stderr or error.stdout or str(error))[-2000:]
            raise HTTPException(status_code=422, detail=f"local speech synthesis failed: {detail}") from error

        content = out_path.read_bytes()

    media_type = "audio/mpeg" if response_format == "mp3" else "audio/wav"
    return Response(content=content, media_type=media_type)
