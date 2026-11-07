use actix_web::{get, web, App, HttpResponse, HttpServer, Responder};
use serde::Serialize;

#[derive(Serialize)]
struct Message {
    message: &'static str,
}

#[get("/text")]
async fn text() -> impl Responder {
    HttpResponse::Ok().content_type("text/plain").body("hello world")
}

#[get("/json")]
async fn json() -> impl Responder {
    web::Json(Message { message: "hello world" })
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let port: u16 = std::env::var("PORT").ok().and_then(|p| p.parse().ok()).unwrap_or(3000);
    let host = std::env::var("HOST").unwrap_or_else(|_| "127.0.0.1".into());
    let workers = std::env::var("WORKERS")
        .ok()
        .and_then(|w| w.parse().ok())
        .unwrap_or_else(|| std::thread::available_parallelism().map(|n| n.get()).unwrap_or(1));

    println!("actix-web listening on :{port} with {workers} workers");
    HttpServer::new(|| App::new().service(text).service(json))
        .workers(workers)
        .bind((host.as_str(), port))?
        .run()
        .await
}
