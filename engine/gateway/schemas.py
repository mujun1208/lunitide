from typing import Literal

from pydantic import BaseModel, Field, HttpUrl, SecretStr, model_validator


class Message(BaseModel):
    role: Literal["system", "user", "assistant"]
    content: str = Field(min_length=1, max_length=50_000)


class ProviderRequest(BaseModel):
    protocol: Literal["openai", "anthropic"]
    baseUrl: HttpUrl
    model: str = Field(min_length=1, max_length=200)
    apiKey: SecretStr = Field(min_length=1, max_length=4096)


class ChatRequest(ProviderRequest):
    messages: list[Message] = Field(min_length=1, max_length=100)
    stream: bool = False

    @model_validator(mode="after")
    def validate_total_content(self):
        if sum(len(message.content) for message in self.messages) > 200_000:
            raise ValueError("message content exceeds request budget")
        return self


class ProviderTestResult(BaseModel):
    ok: bool
    detail: str
    model: str | None = None
    httpStatus: int | None = None
    latencyMs: int | None = None
    checkedAt: str | None = None


class ProviderModelsResult(BaseModel):
    models: list[str]
    detail: str
