import json

import pytest
from english_tutor.conversation.v1 import conversation_pb2

from app.conversation import ConversationService


class RecordingProvider:
    def __init__(self) -> None:
        self.audio = b""

    async def transcribe(self, audio: bytes) -> str:
        self.audio = audio
        return "I would like a coffee."

    async def reply(self, text: str, scene_id: str):
        assert text == "I would like a coffee."
        assert scene_id == "restaurant-order"
        yield "Certainly!"

    async def synthesize(self, text: str):
        assert text == "Certainly!"
        yield b"fake-mp3"


def client_event(turn_seq: int, payload) -> conversation_pb2.ClientEvent:
    return conversation_pb2.ClientEvent(
        session_id="session-1",
        user_id="user-1",
        scene_id="restaurant-order",
        turn_seq=turn_seq,
        ts_ms=1,
        **payload,
    )


async def events(values):
    for value in values:
        yield value


@pytest.mark.asyncio
async def test_audio_turn_preserves_chunk_order_and_streams_reply():
    provider = RecordingProvider()
    service = ConversationService(provider)
    request = events(
        [
            client_event(
                1,
                {
                    "start_turn": conversation_pb2.StartTurn(
                        audio_format="pcm_s16le", sample_rate=16000, channels=1
                    )
                },
            ),
            client_event(
                1,
                {"audio_chunk": conversation_pb2.AudioChunk(data=b"one", chunk_seq=1)},
            ),
            client_event(
                1,
                {"audio_chunk": conversation_pb2.AudioChunk(data=b"two", chunk_seq=2)},
            ),
            client_event(1, {"end_turn": conversation_pb2.EndTurn()}),
        ]
    )

    response = [item async for item in service.StreamChat(request, None)]

    assert provider.audio == b"onetwo"
    assert [item.type for item in response] == [
        conversation_pb2.EVENT_TYPE_ASR_FINAL,
        conversation_pb2.EVENT_TYPE_REPLY_DELTA,
        conversation_pb2.EVENT_TYPE_TTS_START,
        conversation_pb2.EVENT_TYPE_TTS_CHUNK,
        conversation_pb2.EVENT_TYPE_TURN_END,
    ]
    assert json.loads(response[0].payload_json)["turn_seq"] == 1


@pytest.mark.asyncio
async def test_audio_chunk_sequence_error_is_retriable():
    service = ConversationService(RecordingProvider())
    request = events(
        [
            client_event(
                2,
                {
                    "start_turn": conversation_pb2.StartTurn(
                        audio_format="pcm_s16le", sample_rate=16000, channels=1
                    )
                },
            ),
            client_event(
                2,
                {"audio_chunk": conversation_pb2.AudioChunk(data=b"late", chunk_seq=2)},
            ),
        ]
    )

    response = [item async for item in service.StreamChat(request, None)]

    assert len(response) == 1
    payload = json.loads(response[0].payload_json)
    assert response[0].type == conversation_pb2.EVENT_TYPE_ERROR
    assert payload["code"] == "ERR_AUDIO_SEQUENCE"
    assert payload["retriable"] is True


@pytest.mark.asyncio
async def test_rejects_unsupported_audio_format():
    service = ConversationService(RecordingProvider())
    request = events(
        [
            client_event(
                1,
                {
                    "start_turn": conversation_pb2.StartTurn(
                        audio_format="webm", sample_rate=48000, channels=2
                    )
                },
            )
        ]
    )

    response = [item async for item in service.StreamChat(request, None)]

    payload = json.loads(response[0].payload_json)
    assert payload["code"] == "ERR_BAD_AUDIO"
