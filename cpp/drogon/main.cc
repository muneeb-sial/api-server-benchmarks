#include <drogon/drogon.h>

#include <cstdio>
#include <cstdlib>
#include <thread>

using namespace drogon;

using Callback = std::function<void(const HttpResponsePtr &)>;

static int envInt(const char *name, int fallback) {
    const char *v = std::getenv(name);
    return v && *v ? std::atoi(v) : fallback;
}

int main() {
    const int port = envInt("PORT", 3000);
    const char *host = std::getenv("HOST");
    const int workers = envInt("WORKERS", static_cast<int>(std::thread::hardware_concurrency()));

    app().registerHandler("/text", [](const HttpRequestPtr &, Callback &&callback) {
        auto res = HttpResponse::newHttpResponse();
        res->setContentTypeCode(CT_TEXT_PLAIN);
        res->setBody("hello world");
        callback(res);
    }, {Get});

    app().registerHandler("/json", [](const HttpRequestPtr &, Callback &&callback) {
        Json::Value body;
        body["message"] = "hello world";
        callback(HttpResponse::newHttpJsonResponse(body));
    }, {Get});

    std::printf("drogon listening on :%d with %d worker threads\n", port, workers);
    std::fflush(stdout);
    app().setLogLevel(trantor::Logger::kWarn)
        .addListener(host && *host ? host : "127.0.0.1", static_cast<uint16_t>(port))
        .setThreadNum(workers)
        .run();
}
