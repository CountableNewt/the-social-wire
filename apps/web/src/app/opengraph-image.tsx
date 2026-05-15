import { ImageResponse } from "next/og";

export const alt = "The Social Wire";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default function OpengraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          height: "100%",
          width: "100%",
          display: "flex",
          flexDirection: "column",
          alignItems: "flex-start",
          justifyContent: "center",
          background: "linear-gradient(135deg, #0f172a 0%, #1e293b 45%, #0f172a 100%)",
          padding: 72,
          position: "relative",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 20 }}>
          <div
            style={{
              width: 10,
              height: 140,
              background: "#38bdf8",
              borderRadius: 5,
            }}
          />
          <div style={{ display: "flex", flexDirection: "column", gap: 16, maxWidth: 960 }}>
            <div
              style={{
                fontSize: 68,
                fontWeight: 700,
                color: "#f8fafc",
                letterSpacing: "-0.03em",
                lineHeight: 1.05,
              }}
            >
              The Social Wire
            </div>
            <div
              style={{
                fontSize: 30,
                color: "#94a3b8",
                lineHeight: 1.35,
              }}
            >
              A reader for the standard.site publishing ecosystem
            </div>
          </div>
        </div>
        <div
          style={{
            position: "absolute",
            bottom: 56,
            left: 72,
            fontSize: 22,
            color: "#64748b",
            fontWeight: 500,
          }}
        >
          thesocialwire.app
        </div>
      </div>
    ),
    { ...size }
  );
}
