cmake_minimum_required(VERSION 3.10)
project(SoundSwarmClient)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Find libopus (assumes it's installed or provided via vcpkg/conan/ndk)
find_package(PkgConfig REQUIRED)
pkg_check_modules(OPUS REQUIRED opus)

# Core Library
add_library(soundswarm_core STATIC
    core/src/protocol.cpp
    core/src/network.cpp
    core/src/clock_sync.cpp
    core/src/jitter_buffer.cpp
    core/src/codec.cpp
    core/src/engine.cpp
)

target_include_directories(soundswarm_core PUBLIC core/include)
target_include_directories(soundswarm_core PRIVATE ${OPUS_INCLUDE_DIRS})
target_link_libraries(soundswarm_core PRIVATE ${OPUS_LIBRARIES})

if(WIN32)
    target_link_libraries(soundswarm_core PRIVATE ws2_32)
endif()

# Test Client CLI
add_executable(soundswarm_test_client test/main.cpp)
target_link_libraries(soundswarm_test_client PRIVATE soundswarm_core)
