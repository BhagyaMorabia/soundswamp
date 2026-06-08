#import "SoundSwarmWrapper.h"
#include <memory>
#include "engine.h"

@implementation SoundSwarmWrapper {
    std::shared_ptr<soundswarm::Engine> _engine;
}

- (BOOL)connectWithServerIp:(NSString *)ip
                    tcpPort:(NSInteger)tcpPort
                    udpPort:(NSInteger)udpPort
                      token:(NSString *)token
                 deviceName:(NSString *)deviceName {
    
    if (_engine) {
        return YES; // Already connected
    }

    soundswarm::EngineConfig config;
    config.serverIp = [ip UTF8String];
    config.tcpPort = (int)tcpPort;
    config.udpPort = (int)udpPort;
    config.token = [token UTF8String];
    config.deviceName = [deviceName UTF8String];
    config.platform = "ios";
    config.sampleRate = 48000;
    config.channels = 2;

    _engine = std::make_shared<soundswarm::Engine>(config);

    if (!_engine->start()) {
        _engine.reset();
        return NO;
    }

    return YES;
}

- (void)disconnect {
    if (_engine) {
        _engine->stop();
        _engine.reset();
    }
}

- (void)readAudio:(float *)outPcm numFrames:(NSInteger)numFrames playoutTimestampUs:(int64_t)playoutTimestampUs {
    if (_engine) {
        _engine->readAudio(outPcm, numFrames, playoutTimestampUs);
    } else {
        memset(outPcm, 0, numFrames * 2 * sizeof(float));
    }
}

@end
