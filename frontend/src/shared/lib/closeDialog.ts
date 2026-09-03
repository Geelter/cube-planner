// Test environments (jsdom, happy-dom) may lack close() as well as
// showModal() — fall back to clearing the open attribute directly.
export function closeDialog(el: HTMLDialogElement) {
  if (typeof el.close === "function") el.close();
  else el.removeAttribute("open");
}
