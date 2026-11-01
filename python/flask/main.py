from flask import Flask, Response, jsonify

app = Flask(__name__)


@app.get("/text")
def text() -> Response:
    return Response("hello world", mimetype="text/plain")


@app.get("/json")
def json() -> Response:
    return jsonify(message="hello world")
