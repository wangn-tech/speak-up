import asyncio
import tempfile
from collections.abc import AsyncIterator
from http import HTTPStatus
from typing import Protocol

from openai import AsyncOpenAI

from app.config import Settings


class ConversationProvider(Protocol):
    async def reply(self, text: str, scene_id: str) -> AsyncIterator[str]: ...

    async def transcribe(self, audio: bytes) -> str: ...

    async def synthesize(self, text: str) -> AsyncIterator[bytes]: ...


class FakeProvider:
    async def reply(self, text: str, scene_id: str) -> AsyncIterator[str]:
        if scene_id == "job-interview":
            response = (
                f"Thanks for sharing. Could you give me one concrete example related to: {text}?"
            )
        else:
            response = f"Great. I understood: {text}. Would you like anything else?"
        for token in response.split(" "):
            yield token + " "

    async def transcribe(self, audio: bytes) -> str:
        if not audio:
            return ""
        return "I would like to practice speaking English."

    async def synthesize(self, text: str) -> AsyncIterator[bytes]:
        del text
        if False:
            yield b""


class DashScopeProvider(FakeProvider):
    """DashScope LLM, Paraformer ASR, and CosyVoice TTS provider."""

    def __init__(self, settings: Settings) -> None:
        if not settings.dashscope_api_key:
            raise ValueError("DASHSCOPE_API_KEY is required when AI_PROVIDER=dashscope")
        self._model = settings.qwen_chat_model
        self._api_key = settings.dashscope_api_key
        self._ws_url = settings.dashscope_ws_url
        self._asr_model = settings.asr_model_realtime
        self._tts_model = settings.tts_model
        self._tts_voice = settings.tts_voice
        self._client = AsyncOpenAI(
            api_key=settings.dashscope_api_key,
            base_url=settings.dashscope_base_url,
        )

    async def reply(self, text: str, scene_id: str) -> AsyncIterator[str]:
        stream = await self._client.chat.completions.create(
            model=self._model,
            messages=[
                {
                    "role": "system",
                    "content": (
                        "You are an English speaking partner. Stay in the selected scene, "
                        "reply in concise natural English, and ask one useful follow-up question. "
                        f"Scene: {scene_id}."
                    ),
                },
                {"role": "user", "content": text},
            ],
            stream=True,
            timeout=15,
        )
        async for chunk in stream:
            delta = chunk.choices[0].delta.content if chunk.choices else None
            if delta:
                yield delta

    async def transcribe(self, audio: bytes) -> str:
        return await asyncio.to_thread(self._transcribe_sync, audio)

    def _transcribe_sync(self, audio: bytes) -> str:
        import dashscope
        from dashscope.audio.asr import Recognition

        dashscope.api_key = self._api_key
        dashscope.base_websocket_api_url = self._ws_url
        with tempfile.NamedTemporaryFile(suffix=".pcm") as source:
            source.write(audio)
            source.flush()
            recognition = Recognition(
                model=self._asr_model,
                format="pcm",
                sample_rate=16000,
                language_hints=["zh", "en"],
                callback=None,
            )
            result = recognition.call(source.name)
        if result.status_code != HTTPStatus.OK:
            raise RuntimeError(f"DashScope ASR failed: {result.message}")
        return " ".join(
            sentence.get("text", "") for sentence in result.get_sentence() if sentence.get("text")
        )

    async def synthesize(self, text: str) -> AsyncIterator[bytes]:
        audio = await asyncio.to_thread(self._synthesize_sync, text)
        if audio:
            yield audio

    def _synthesize_sync(self, text: str) -> bytes:
        import dashscope
        from dashscope.audio.tts_v2 import SpeechSynthesizer

        dashscope.api_key = self._api_key
        dashscope.base_websocket_api_url = self._ws_url
        synthesizer = SpeechSynthesizer(model=self._tts_model, voice=self._tts_voice)
        return synthesizer.call(text)


def build_provider(settings: Settings) -> ConversationProvider:
    if settings.ai_provider == "dashscope":
        return DashScopeProvider(settings)
    return FakeProvider()
