export class CompressedVideoTooLargeError extends Error {
  constructor() { super("The Compressed Video Still Exceeds The Size Limit."); }
}

/** Random-access writes are required: MP4 muxers can rewrite headers after media data. */
export function boundedVideoOutput(limit: number, pageSize = 1024 * 1024) {
  if (!Number.isSafeInteger(limit) || limit <= 0 || !Number.isSafeInteger(pageSize) || pageSize <= 0) throw new Error("Invalid Video Output Limit.");
  const pages: Uint8Array<ArrayBuffer>[] = [];
  let length = 0, closed = false, aborted = false;
  const writable = new WritableStream<{ data: Uint8Array; position: number }>({
    write({ data, position }) {
      const end = position + data.byteLength;
      if (!Number.isSafeInteger(position) || position < 0 || !Number.isSafeInteger(end) || end > limit) throw new CompressedVideoTooLargeError();
      if (!data.byteLength) return;
      while (pages.length * pageSize < end) pages.push(new Uint8Array(Math.min(pageSize, limit - pages.length * pageSize)));
      for (let sourceOffset = 0; sourceOffset < data.byteLength;) {
        const destination = position + sourceOffset;
        const page = pages[Math.floor(destination / pageSize)];
        const withinPage = destination % pageSize;
        const count = Math.min(data.byteLength - sourceOffset, page.length - withinPage);
        page.set(data.subarray(sourceOffset, sourceOffset + count), withinPage);
        sourceOffset += count;
      }
      length = Math.max(length, end);
    },
    close() { closed = true; },
    abort() { aborted = true; pages.length = 0; },
  });
  return {
    writable,
    get byteLength() { return length; },
    toBlob() {
      if (!closed || aborted) throw new Error("Video Compression Did Not Finish.");
      return new Blob(pages.map((page, index) => page.subarray(0, Math.min(page.length, length - index * pageSize))), { type: "video/mp4" });
    },
  };
}
