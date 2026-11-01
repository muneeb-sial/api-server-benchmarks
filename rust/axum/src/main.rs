use axum::{routing::get, Json, Router};
use serde::Serialize;

#[derive(Serialize)]
struct Message {
    message: &'static str,
}

async fn text() -> &'static str {
    "hello world"
}

async fn json() -> Json<Message> {
    Json(Message { message: "hello world" })
}

fn main() {
    let port = std::env::var("PORT").unwrap_or_else(|_| "3000".into());
    let workers = std::env::var("WORKERS")
        .ok()
        .and_then(|w| w.parse().ok())
        .unwrap_or_else(|| std::thread::available_parallelism().map(|n| n.get()).unwrap_or(1));

    tokio::runtime::Builder::new_multi_thread()
        .worker_threads(workers)
        .enable_all()
        .build()
        .unwrap()
        .block_on(async {
            let app = Router::new().route("/text", get(text)).route("/json", get(json));
            let listener = tokio::net::TcpListener::bind(format!("127.0.0.1:{port}")).await.unwrap();
            println!("axum listening on :{port} with {workers} worker threads");
            axum::serve(listener, app).await.unwrap();
        });
}
