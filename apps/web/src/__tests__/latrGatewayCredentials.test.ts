import { afterEach, describe, expect, it } from "bun:test";

import {
  hasLatrGatewayClientCredentials,
  isLatrGatewayAuthRejected,
  isLatrGatewayInvalidClientCredentialResponse,
  latrGatewayClientAuthHeaderNames,
  markLatrGatewayAuthRejected,
  resetLatrGatewayAuthRejectedForTests,
} from "@/lib/latrGatewayCredentials";

const originalClientId = process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID;
const originalApiKey = process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY;
const originalCredential = process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL;

afterEach(() => {
  resetLatrGatewayAuthRejectedForTests();
  if (originalClientId === undefined) {
    delete process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID;
  } else {
    process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID = originalClientId;
  }
  if (originalApiKey === undefined) {
    delete process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY;
  } else {
    process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY = originalApiKey;
  }
  if (originalCredential === undefined) {
    delete process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL;
  } else {
    process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL = originalCredential;
  }
});

describe("latrGatewayCredentials", () => {
  it("detects split developer credentials", () => {
    delete process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL;
    process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID = "the-social-wire-web";
    process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY = "lk_test";
    expect(hasLatrGatewayClientCredentials()).toBe(true);
    expect(latrGatewayClientAuthHeaderNames()).toEqual([
      "X-Latr-Client-Id",
      "X-Latr-API-Key",
    ]);
  });

  it("detects official client credential fallback", () => {
    delete process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID;
    delete process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY;
    process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL = "dGVzdA==";
    expect(hasLatrGatewayClientCredentials()).toBe(true);
    expect(latrGatewayClientAuthHeaderNames()).toEqual(["X-Latr-Official-Client"]);
  });

  it("requires both developer headers before treating credentials as configured", () => {
    delete process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL;
    process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID = "the-social-wire-web";
    delete process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY;
    expect(hasLatrGatewayClientCredentials()).toBe(false);
  });

  it("recognizes invalid client credential responses", () => {
    expect(
      isLatrGatewayInvalidClientCredentialResponse(403, {
        error: "invalid_client_credential",
        message: "Invalid gateway client credentials",
      })
    ).toBe(true);
    expect(isLatrGatewayInvalidClientCredentialResponse(401, {})).toBe(false);
  });

  it("marks auth rejected for circuit breaker", () => {
    markLatrGatewayAuthRejected();
    expect(isLatrGatewayAuthRejected()).toBe(true);
  });
});
