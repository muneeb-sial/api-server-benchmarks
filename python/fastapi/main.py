from fastapi import FastAPI
from fastapi.responses import PlainTextResponse

app = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)


@app.get("/text", response_class=PlainTextResponse)
async def text() -> str:
    return "hello world"


@app.get("/json")
async def json() -> dict:
    return {"message": "hello world"}
