export function downloadTextFile(
  filename: string,
  text: string,
  mime = "text/plain;charset=utf-8",
): void {
  const blob = new Blob([text], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
