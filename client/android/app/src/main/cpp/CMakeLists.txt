cmake_minimum_required(VERSION 3.22.1)
project(soundswarm_jni)

set(CMAKE_CXX_STANDARD 17)

# Oboe (via Prefab)
find_package(oboe REQUIRED CONFIG)

# Opus (via FetchContent)
include(FetchContent)
FetchContent_Declare(
    opus
    GIT_REPOSITORY https://github.com/xiph/opus.git
    GIT_TAG v1.4
)
# Disable Opus programs/tests to speed up build
set(OPUS_BUILD_PROGRAMS OFF CACHE BOOL "" FORCE)
set(OPUS_BUILD_TESTING OFF CACHE BOOL "" FORCE)
set(OPUS_BUILD_SHARED_LIBRARY OFF CACHE BOOL "" FORCE)
FetchContent_MakeAvailable(opus)

# Core library source files
set(CORE_SRC
    ../../../../../core/src/clock_sync.cpp
    ../../../../../core/src/codec.cpp
    ../../../../../core/src/engine.cpp
    ../../../../../core/src/jitter_buffer.cpp
    ../../../../../core/src/network.cpp
    ../../../../../core/src/protocol.cpp
)

# Android JNI source files
set(JNI_SRC
    ../../../../jni/audio_player.cpp
    ../../../../jni/soundswarm_jni.cpp
)

# Add shared library for Android
add_library(soundswarm_jni SHARED ${CORE_SRC} ${JNI_SRC})

# Includes
target_include_directories(soundswarm_jni PRIVATE
    ../../../../../core/include
    ../../../../jni
    ${opus_SOURCE_DIR}/include
)

# Link
target_link_libraries(soundswarm_jni
    android
    log
    oboe::oboe
    opus
)
