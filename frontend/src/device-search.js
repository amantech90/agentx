export function createDeviceSearch({
  durationMs = 30_000,
  schedule = globalThis.setTimeout,
  cancel = globalThis.clearTimeout,
  onChange = () => {},
} = {}) {
  let currentStatus = "idle";
  let remaining = 0;
  let timer = null;
  let generation = 0;

  function scheduleTick(activeGeneration) {
    timer = schedule(() => {
      timer = null;
      if (currentStatus !== "searching" || activeGeneration !== generation) return;
      remaining = Math.max(0, remaining - 1);
      if (remaining === 0) {
        currentStatus = "finished";
        onChange(currentStatus, remaining);
        return;
      }
      onChange(currentStatus, remaining);
      scheduleTick(activeGeneration);
    }, 1_000);
  }

  function start() {
    if (timer !== null) cancel(timer);
    generation += 1;
    currentStatus = "searching";
    remaining = Math.max(1, Math.ceil(durationMs / 1_000));
    onChange(currentStatus, remaining);
    scheduleTick(generation);
  }

  function stop() {
    if (timer !== null) cancel(timer);
    generation += 1;
    timer = null;
    remaining = 0;
    currentStatus = "finished";
    onChange(currentStatus, remaining);
  }

  return {
    start,
    retry: start,
    stop,
    status: () => currentStatus,
    remainingSeconds: () => remaining,
  };
}
