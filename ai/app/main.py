import asyncio
import logging
import signal

import grpc
from english_tutor.conversation.v1 import conversation_pb2_grpc

from app.config import Settings
from app.conversation import ConversationService
from app.evaluation import run_evaluation_worker
from app.provider import build_provider


def log_task_failure(task: asyncio.Task) -> None:
    if task.cancelled():
        return
    if error := task.exception():
        logging.error(
            "Background worker stopped",
            exc_info=(type(error), error, error.__traceback__),
        )


async def serve() -> None:
    logging.basicConfig(level=logging.INFO)
    settings = Settings()
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for signal_name in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(signal_name, stop.set)

    server = grpc.aio.server()
    conversation_pb2_grpc.add_ConversationServiceServicer_to_server(
        ConversationService(build_provider(settings)), server
    )
    server.add_insecure_port(settings.ai_grpc_bind)
    await server.start()
    logging.info("AI gRPC listening on %s", settings.ai_grpc_bind)

    worker = asyncio.create_task(run_evaluation_worker(settings, stop))
    worker.add_done_callback(log_task_failure)
    await stop.wait()
    worker.cancel()
    await asyncio.gather(worker, return_exceptions=True)
    await server.stop(grace=3)


if __name__ == "__main__":
    asyncio.run(serve())
