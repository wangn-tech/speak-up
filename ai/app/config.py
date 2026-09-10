from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file="../.env", extra="ignore")

    ai_grpc_bind: str = "127.0.0.1:50051"
    ai_provider: str = "fake"
    kafka_brokers: str = "127.0.0.1:19092"
    dashscope_api_key: str = ""
    dashscope_base_url: str = "https://dashscope.aliyuncs.com/compatible-mode/v1"
    dashscope_ws_url: str = "wss://dashscope.aliyuncs.com/api-ws/v1/inference"
    qwen_chat_model: str = "qwen3.7-plus"
    asr_model_realtime: str = "paraformer-realtime-v2"
    tts_model: str = "cosyvoice-v2"
    tts_voice: str = "longxiaochun_v2"

    @property
    def broker_list(self) -> list[str]:
        return [item.strip() for item in self.kafka_brokers.split(",") if item.strip()]
