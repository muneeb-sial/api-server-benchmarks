cmake_minimum_required(VERSION 3.20)
project(drogon_bench CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
# Static CRT, to match the x64-windows-static-release vcpkg triplet.
set(CMAKE_MSVC_RUNTIME_LIBRARY "MultiThreaded")
set(CMAKE_INTERPROCEDURAL_OPTIMIZATION ON)

find_package(Drogon CONFIG REQUIRED)

add_executable(drogon-bench main.cc)
target_link_libraries(drogon-bench PRIVATE Drogon::Drogon)
