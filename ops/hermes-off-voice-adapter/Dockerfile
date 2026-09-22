FROM mimir-transcription:1.0.0

COPY voice_adapter.py /voice-adapter/voice_adapter.py

ENV MIMIR_TRANSCRIBE_URL=http://172.17.0.1:8091/transcribe
ENV VOICE_ADAPTER_TTS_VOICE=slt
ENV VOICE_ADAPTER_OUTPUT_FORMAT=mp3

EXPOSE 8080

CMD ["uvicorn", "voice_adapter:app", "--host", "0.0.0.0", "--port", "8080", "--app-dir", "/voice-adapter"]
