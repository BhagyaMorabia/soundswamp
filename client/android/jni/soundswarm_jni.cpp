#include <jni.h>
#include <string>
#include <memory>
#include "engine.h"
#include "audio_player.h"

// Global pointers for the JNI wrapper
static std::shared_ptr<soundswarm::Engine> g_engine;
static std::unique_ptr<soundswarm::android::AudioPlayer> g_audioPlayer;

// Helper to convert jstring to std::string
std::string jstring2string(JNIEnv *env, jstring jStr) {
    if (!jStr) return "";
    const char *chars = env->GetStringUTFChars(jStr, nullptr);
    std::string ret(chars);
    env->ReleaseStringUTFChars(jStr, chars);
    return ret;
}

extern "C" JNIEXPORT jboolean JNICALL
Java_com_soundswarm_client_SoundSwarmClient_nativeConnect(
        JNIEnv* env,
        jobject /* this */,
        jstring jIp,
        jint tcpPort,
        jint udpPort,
        jstring jToken,
        jstring jDeviceName) {
    
    try {
        if (g_engine) {
            // Already connected
            return JNI_TRUE;
        }

        soundswarm::EngineConfig config;
        config.serverIp = jstring2string(env, jIp);
        config.tcpPort = tcpPort;
        config.udpPort = udpPort;
        config.token = jstring2string(env, jToken);
        config.deviceName = jstring2string(env, jDeviceName);
        config.platform = "android";
        config.sampleRate = 48000;
        config.channels = 2;

        g_engine = std::make_shared<soundswarm::Engine>(config);

        if (!g_engine->start()) {
            g_engine.reset();
            return JNI_FALSE;
        }

        // Start Oboe Audio Player
        g_audioPlayer = std::make_unique<soundswarm::android::AudioPlayer>(g_engine);
        if (!g_audioPlayer->start()) {
            g_engine->stop();
            g_engine.reset();
            g_audioPlayer.reset();
            return JNI_FALSE;
        }

        return JNI_TRUE;
    } catch (const std::exception& e) {
        if (g_audioPlayer) g_audioPlayer.reset();
        if (g_engine) g_engine.reset();
        return JNI_FALSE;
    }
}

extern "C" JNIEXPORT void JNICALL
Java_com_soundswarm_client_SoundSwarmClient_nativeDisconnect(
        JNIEnv* env,
        jobject /* this */) {
    
    if (g_audioPlayer) {
        g_audioPlayer->stop();
        g_audioPlayer.reset();
    }

    if (g_engine) {
        g_engine->stop();
        g_engine.reset();
    }
}
