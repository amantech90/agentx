#ifndef AGENTX_APPNAP_H
#define AGENTX_APPNAP_H

// Hold a background activity for the process lifetime so macOS App Nap does not
// suspend Agent X's timers and networking when the window is not focused.
void agentxDisableAppNap(void);

#endif
