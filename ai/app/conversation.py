import asyncio
import base64
import json
import time
import uuid
from collections import defaultdict

import grpc
from english_tutor.conversation.v1 import conversation_pb2, conversation_pb2_grpc

from app.provider import ConversationProvider


class ConversationService(conversation_pb2_grpc.ConversationServiceServicer):
    def __init__(self, provider: ConversationProvider) -> None:
        self._provider = provider

    async def StreamChat(self, request_iterator, context: grpc.aio.ServicerContext):
        audio_buffers: dict[int, bytearray] = defaultdict(bytearray)
        audio_sequences: dict[int, int] = {}
        async for event in request_iterator:
            payload = event.WhichOneof("payload")
            if payload == "start_turn":
                if (
                    event.start_turn.audio_format != "pcm_s16le"
                    or event.start_turn.sample_rate != 16000
                    or event.start_turn.channels != 1
                ):
                    yield self._event(
                        event,
                        conversation_pb2.EVENT_TYPE_ERROR,
                        {
                            "code": "ERR_BAD_AUDIO",
                            "message": "Only PCM 16kHz mono is supported",
                            "retriable": False,
                        },
                    )
                    return
                audio_buffers[event.turn_seq] = bytearray()
                audio_sequences[event.turn_seq] = 0
            elif payload == "audio_chunk":
                expected = audio_sequences.get(event.turn_seq, 0) + 1
                if event.turn_seq not in audio_sequences or event.audio_chunk.chunk_seq != expected:
                    audio_buffers.pop(event.turn_seq, None)
                    audio_sequences.pop(event.turn_seq, None)
                    yield self._event(
                        event,
                        conversation_pb2.EVENT_TYPE_ERROR,
                        {
                            "code": "ERR_AUDIO_SEQUENCE",
                            "message": f"Expected audio chunk {expected}",
                            "retriable": True,
                            "turn_seq": event.turn_seq,
                        },
                    )
                    continue
                audio_buffers[event.turn_seq].extend(event.audio_chunk.data)
                audio_sequences[event.turn_seq] = event.audio_chunk.chunk_seq
            elif payload == "end_turn":
                audio_sequences.pop(event.turn_seq, None)
                try:
                    async with asyncio.timeout(20):
                        text = await self._provider.transcribe(
                            bytes(audio_buffers.pop(event.turn_seq, b""))
                        )
                except Exception as error:
                    yield self._event(
                        event,
                        conversation_pb2.EVENT_TYPE_ERROR,
                        {
                            "code": "ERR_ASR_TIMEOUT",
                            "message": str(error),
                            "retriable": True,
                            "turn_seq": event.turn_seq,
                        },
                    )
                    continue
                if not text:
                    yield self._event(
                        event,
                        conversation_pb2.EVENT_TYPE_ERROR,
                        {
                            "code": "ERR_ASR_EMPTY",
                            "message": "No speech detected",
                            "retriable": True,
                        },
                    )
                    continue
                yield self._event(
                    event,
                    conversation_pb2.EVENT_TYPE_ASR_FINAL,
                    {"text": text, "is_final": True, "turn_seq": event.turn_seq},
                )
                async for response in self._respond(event, text):
                    yield response
            elif payload == "text_input":
                async for response in self._respond(event, event.text_input.text):
                    yield response

    async def _respond(self, event, text: str):
        started = time.monotonic()
        reply_parts: list[str] = []
        try:
            async with asyncio.timeout(20):
                async for delta in self._provider.reply(text, event.scene_id):
                    reply_parts.append(delta)
                    yield self._event(
                        event,
                        conversation_pb2.EVENT_TYPE_REPLY_DELTA,
                        {"delta": delta, "turn_seq": event.turn_seq},
                    )
        except Exception:
            fallback = "Let's keep going. Could you say that in another way?"
            reply_parts.append(fallback)
            yield self._event(
                event,
                conversation_pb2.EVENT_TYPE_REPLY_DELTA,
                {"delta": fallback, "turn_seq": event.turn_seq, "degraded": True},
            )

        reply = "".join(reply_parts).strip()
        yield self._event(
            event,
            conversation_pb2.EVENT_TYPE_TTS_START,
            {"voice": "default", "turn_seq": event.turn_seq},
        )
        try:
            async for audio in self._provider.synthesize(reply):
                yield self._event(
                    event,
                    conversation_pb2.EVENT_TYPE_TTS_CHUNK,
                    {
                        "audio_base64": base64.b64encode(audio).decode(),
                        "format": "mp3",
                        "sample_rate": 24000,
                        "turn_seq": event.turn_seq,
                    },
                )
        except Exception as error:
            yield self._event(
                event,
                conversation_pb2.EVENT_TYPE_ERROR,
                {
                    "code": "ERR_TTS_TIMEOUT",
                    "message": str(error),
                    "retriable": True,
                    "turn_seq": event.turn_seq,
                },
            )
        yield self._event(
            event,
            conversation_pb2.EVENT_TYPE_TURN_END,
            {"turn_seq": event.turn_seq, "duration_ms": int((time.monotonic() - started) * 1000)},
        )

    @staticmethod
    def _event(client_event, event_type: int, payload: dict) -> conversation_pb2.ServerEvent:
        return conversation_pb2.ServerEvent(
            session_id=client_event.session_id,
            turn_id=str(uuid.uuid4()),
            type=event_type,
            payload_json=json.dumps(payload, ensure_ascii=False),
            ts_ms=int(time.time() * 1000),
        )
