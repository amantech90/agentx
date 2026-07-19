export function createNoticeState({
  schedule = (callback, delay) => window.setTimeout(callback, delay),
  cancel = (timer) => window.clearTimeout(timer),
  onChange = () => {},
} = {}) {
  let notice = null;
  let timer = null;

  function current() {
    return notice ? { ...notice } : null;
  }

  return {
    current,
    show(message, isError = false) {
      if (timer !== null) cancel(timer);
      notice = { message: String(message || "Something went wrong."), isError: Boolean(isError) };
      onChange(current());
      timer = schedule(() => {
        timer = null;
        notice = null;
        onChange(null);
      }, 4200);
    },
  };
}
