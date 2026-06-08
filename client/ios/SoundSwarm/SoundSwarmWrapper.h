#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

@interface SoundSwarmWrapper : NSObject

// Initialize and connect to the server
- (BOOL)connectWithServerIp:(NSString *)ip
                    tcpPort:(NSInteger)tcpPort
                    udpPort:(NSInteger)udpPort
                      token:(NSString *)token
                 deviceName:(NSString *)deviceName;

// Disconnect and shutdown the engine
- (void)disconnect;

// Pull synchronized audio. Called repeatedly by the AVAudioEngine render callback.
// outPcm: pre-allocated buffer to fill
// numFrames: number of frames requested
// playoutTimestampUs: exact physical time the first frame hits the DAC
- (void)readAudio:(float *)outPcm numFrames:(NSInteger)numFrames playoutTimestampUs:(int64_t)playoutTimestampUs;

@end

NS_ASSUME_NONNULL_END
