/** Podcast clips were retired; old public links report that the content is gone. */
function gone(): Response {
  return new Response("This podcast clip is no longer available.", {
    status: 410,
    headers: { "Cache-Control": "public, max-age=86400", "Content-Type": "text/plain; charset=utf-8", "X-Robots-Tag": "noindex" },
  });
}

export const GET = gone;
export const HEAD = gone;
