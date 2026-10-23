import crypto from "node:crypto";
import { cookies } from "next/headers";
import { getDb, verifyPassword } from "./db";
import { getUser } from "./users";
import type { User } from "./types";

const SECRET = process.env.SESSION_SECRET || "faka-demo-session-secret";
export const SESSION_COOKIE = "faka_session";

function sign(uid: number): string {
  const h = crypto.createHmac("sha256", SECRET).update(String(uid)).digest("hex").slice(0, 24);
  return `${uid}.${h}`;
}

export function verifySession(token: string | undefined | null): number | null {
  if (!token) return null;
  const [uidStr, sig] = token.split(".");
  const uid = Number(uidStr);
  if (!uid || !sig) return null;
  const expected = sign(uid).split(".")[1];
  if (sig.length !== expected.length) return null;
  if (!crypto.timingSafeEqual(Buffer.from(sig), Buffer.from(expected))) return null;
  return uid;
}

export function sessionToken(uid: number): string {
  return sign(uid);
}

export function authenticate(username: string, password: string): User | null {
  const row = getDb()
    .prepare("SELECT uid, password FROM users WHERE username = ?")
    .get(username) as { uid: number; password: string } | undefined;
  if (!row) return null;
  if (!verifyPassword(password, row.password)) return null;
  return getUser(row.uid);
}

/** 当前登录用户（服务端） */
export async function getSessionUser(): Promise<User | null> {
  const store = await cookies();
  const uid = verifySession(store.get(SESSION_COOKIE)?.value);
  if (!uid) return null;
  return getUser(uid);
}

export async function requireUser(): Promise<User> {
  const u = await getSessionUser();
  if (!u) throw new Error("UNAUTHORIZED");
  return u;
}

export async function requireAdmin(): Promise<User> {
  const u = await getSessionUser();
  if (!u || u.role !== "admin") throw new Error("FORBIDDEN");
  return u;
}
