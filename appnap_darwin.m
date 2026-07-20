#import <Foundation/Foundation.h>
#include "appnap_darwin.h"

// NSActivityBackground prevents App Nap while still allowing normal system
// sleep. The returned activity token is intentionally retained forever so the
// assertion lasts as long as the app runs.
void agentxDisableAppNap(void) {
    NSActivityOptions options = NSActivityBackground;
    id token = [[NSProcessInfo processInfo]
        beginActivityWithOptions:options
                          reason:@"Agent X keeps paired devices connected"];
    if (token != nil) {
        CFBridgingRetain(token);
    }
}
