/** Browser side of WebAuthn: converts between the server's base64url JSON and the binary browser API. */

export class PasskeyError extends Error {
  constructor(public readonly reason: "unsupported" | "cancelled" | "failed") {
    super(reason);
    this.name = "PasskeyError";
  }
}

export function passkeysSupported(): boolean {
  return typeof window !== "undefined" && !!window.PublicKeyCredential && !!navigator.credentials && window.isSecureContext;
}

export function b64uEncode(buf: ArrayBuffer): string {
  let s = "";
  for (const b of new Uint8Array(buf)) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function b64uDecode(s: string): ArrayBuffer {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

type Json = Record<string, unknown>;
type Desc = { type: string; id: string };

const descriptors = (v: unknown): PublicKeyCredentialDescriptor[] =>
  ((v as Desc[] | undefined) ?? []).map((d) => ({ type: "public-key", id: b64uDecode(d.id) }));

function wrap<T>(p: Promise<T | null>): Promise<T> {
  return p.then((r) => {
    if (!r) throw new PasskeyError("cancelled");
    return r;
  }, (e: unknown) => {
    const name = e instanceof DOMException ? e.name : "";
    throw new PasskeyError(name === "NotAllowedError" || name === "AbortError" ? "cancelled" : "failed");
  });
}

/** navigator.credentials.create() from the server's options; returns the JSON the API expects. */
export async function createPasskey(options: Json): Promise<unknown> {
  if (!passkeysSupported()) throw new PasskeyError("unsupported");
  const o = options as Json & { user: Json; challenge: string };
  const cred = (await wrap(navigator.credentials.create({
    publicKey: {
      ...(o as unknown as PublicKeyCredentialCreationOptions),
      challenge: b64uDecode(o.challenge),
      user: { ...(o.user as unknown as PublicKeyCredentialUserEntity), id: b64uDecode(String(o.user.id)) },
      excludeCredentials: descriptors(o.excludeCredentials),
    },
  }))) as PublicKeyCredential;
  const r = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: b64uEncode(cred.rawId),
    response: { attestationObject: b64uEncode(r.attestationObject), clientDataJSON: b64uEncode(r.clientDataJSON) },
  };
}

/** navigator.credentials.get() from the server's options; returns the JSON the API expects. */
export async function getPasskey(options: Json): Promise<unknown> {
  if (!passkeysSupported()) throw new PasskeyError("unsupported");
  const o = options as Json & { challenge: string };
  const cred = (await wrap(navigator.credentials.get({
    publicKey: {
      ...(o as unknown as PublicKeyCredentialRequestOptions),
      challenge: b64uDecode(o.challenge),
      allowCredentials: descriptors(o.allowCredentials),
    },
  }))) as PublicKeyCredential;
  const r = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: b64uEncode(cred.rawId),
    response: {
      authenticatorData: b64uEncode(r.authenticatorData),
      clientDataJSON: b64uEncode(r.clientDataJSON),
      signature: b64uEncode(r.signature),
    },
  };
}
