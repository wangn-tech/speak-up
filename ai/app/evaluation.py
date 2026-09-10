import asyncio
import json
import logging
import uuid

from aiokafka import AIOKafkaConsumer, AIOKafkaProducer

from app.config import Settings

logger = logging.getLogger(__name__)


def evaluate(trigger: dict) -> dict:
    transcript = trigger.get("transcript") or []
    user_text = " ".join(item.get("user_text", "") for item in transcript).strip()
    words = len(user_text.split())
    base = min(92, 55 + words * 2) if words else 0
    dimensions = {
        "pronunciation": max(0, base - 3),
        "grammar": base,
        "vocabulary": min(100, base + 2),
        "fluency": max(0, base - 1),
        "coherence": min(100, base + 1),
    }
    overall = round(sum(dimensions.values()) / len(dimensions), 1) if words else 0
    return {
        "overall_score": overall,
        "dimensions": dimensions,
        "highlights": ["You completed a multi-turn English conversation."] if words else [],
        "issues": [] if words else ["No effective speech or text was detected."],
        "suggestions": [
            "Use one concrete detail in your next answer.",
            "Link ideas with because, so, or however.",
        ]
        if words
        else ["Try one short sentence to begin."],
        "experimental_pronunciation": True,
    }


async def run_evaluation_worker(settings: Settings, stop: asyncio.Event) -> None:
    if not settings.broker_list:
        return
    consumer = AIOKafkaConsumer(
        "evaluation.trigger",
        bootstrap_servers=settings.broker_list,
        group_id="speakup-ai",
        enable_auto_commit=False,
        auto_offset_reset="earliest",
    )
    producer = AIOKafkaProducer(bootstrap_servers=settings.broker_list)
    while not stop.is_set():
        try:
            await consumer.start()
            await producer.start()
            break
        except Exception:
            logger.exception("Kafka is not ready; retrying")
            await consumer.stop()
            await producer.stop()
            await asyncio.sleep(2)
    try:
        async for message in consumer:
            trigger = json.loads(message.value)
            completed = {
                "event_id": trigger["event_id"],
                "task_id": str(uuid.uuid4()),
                "session_id": trigger["session_id"],
                "user_id": trigger["user_id"],
                "status": "done",
                "result_json": evaluate(trigger),
                "schema_version": "1",
            }
            await producer.send_and_wait(
                "evaluation.completed",
                json.dumps(completed, ensure_ascii=False).encode(),
                key=trigger["session_id"].encode(),
            )
            await consumer.commit()
            if stop.is_set():
                break
    finally:
        await consumer.stop()
        await producer.stop()
