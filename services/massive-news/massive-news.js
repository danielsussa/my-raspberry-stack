#!/usr/bin/env node
/* eslint-disable no-console */
"use strict";

const fs = require("fs");
const { URL, URLSearchParams } = require("url");

const API_KEY = process.env.MASSIVE_API_KEY || "";
const BASE_URL =
  process.env.MASSIVE_NEWS_URL || "https://api.massive.com/v2/reference/news";
const ORDER = process.env.MASSIVE_NEWS_ORDER || "desc";
const SORT = process.env.MASSIVE_NEWS_SORT || "published_utc";
const LIMIT = process.env.MASSIVE_NEWS_LIMIT || "10";
const EXTRA_QUERY = process.env.MASSIVE_NEWS_QUERY || "";
const OUT_FILE = process.env.MASSIVE_NEWS_OUT_FILE || "";
const OUT_DIR = process.env.MASSIVE_NEWS_OUT_DIR || "/data";

if (!API_KEY) {
  console.error("MASSIVE_API_KEY is required");
  process.exit(1);
}

const url = new URL(BASE_URL);
const params = new URLSearchParams(url.search);
params.set("order", ORDER);
params.set("sort", SORT);
params.set("limit", LIMIT);
params.set("apiKey", API_KEY);
if (EXTRA_QUERY) {
  for (const pair of EXTRA_QUERY.split("&")) {
    if (!pair) continue;
    const [k, v = ""] = pair.split("=");
    if (k) params.set(k, v);
  }
}
url.search = params.toString();

const toIso = (value) => {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return String(value);
  return d.toISOString();
};

const pick = (obj, keys) => {
  for (const k of keys) {
    if (!obj) break;
    const v = obj[k];
    if (v !== undefined && v !== null && v !== "") return v;
  }
  return "";
};

const getSource = (item) => {
  if (!item) return "";
  if (typeof item.source === "string") return item.source;
  if (item.source && typeof item.source.name === "string") return item.source.name;
  if (typeof item.publisher === "string") return item.publisher;
  if (item.publisher && typeof item.publisher.name === "string")
    return item.publisher.name;
  return "";
};

const getUrl = (item) =>
  pick(item, ["article_url", "url", "link", "amp_url", "homepage_url"]);

const getSummary = (item) =>
  pick(item, ["summary", "description", "snippet", "teaser"]);

const getTickers = (item) => {
  if (!item) return [];
  if (Array.isArray(item.tickers)) return item.tickers.filter(Boolean);
  if (Array.isArray(item.ticker)) return item.ticker.filter(Boolean);
  if (typeof item.ticker === "string") return [item.ticker];
  return [];
};

const getKeywords = (item) => {
  if (!item) return [];
  if (Array.isArray(item.keywords)) return item.keywords.filter(Boolean);
  if (typeof item.keywords === "string")
    return item.keywords.split(",").map((k) => k.trim()).filter(Boolean);
  return [];
};

const getInsights = (item) => {
  if (!item || !Array.isArray(item.insights)) return [];
  const out = [];
  for (const ins of item.insights) {
    if (!ins || typeof ins !== "object") continue;
    const parts = [];
    if (ins.ticker) parts.push(String(ins.ticker));
    if (ins.sentiment) parts.push(String(ins.sentiment));
    if (ins.sentiment_reasoning)
      parts.push(String(ins.sentiment_reasoning));
    if (parts.length) out.push(parts.join(": "));
  }
  return out;
};

const makeKeys = (item) => {
  const id = pick(item, ["id", "uuid", "_id", "news_id"]);
  const url = getUrl(item);
  const title = pick(item, ["title", "headline", "name"]) || "";
  const published = toIso(
    pick(item, ["published_utc", "published_at", "published", "created_at", "timestamp"])
  );
  const keys = [];
  if (id) keys.push(`id:${id}`);
  if (url) keys.push(`url:${url}`);
  if (title && published) keys.push(`tp:${title}|${published}`);
  if (title) keys.push(`t:${title}`);
  return keys;
};

const normalizeText = (value) =>
  String(value || "")
    .replace(/\s+/g, " ")
    .trim();

const safeFilename = (value) =>
  String(value || "")
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");

async function main() {
  const res = await fetch(url.toString(), {
    headers: { "User-Agent": "massive-news/1.0" },
  });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    console.error(`Massive API error: ${res.status} ${res.statusText}`);
    if (body) console.error(body);
    process.exit(1);
  }

  const data = await res.json();
  const items =
    data?.results ||
    data?.data ||
    data?.news ||
    data?.items ||
    (Array.isArray(data) ? data : []);

  if (!Array.isArray(items)) {
    console.error("Unexpected API response shape");
    process.exit(1);
  }

  const now = new Date();
  const nowIso = now.toISOString();
  const y = String(now.getFullYear());
  const m = String(now.getMonth() + 1).padStart(2, "0");
  const d = String(now.getDate()).padStart(2, "0");
  const dateStamp = `${y}-${m}-${d}`;
  const dateDir = OUT_FILE || `${OUT_DIR}/${dateStamp}`;
  fs.mkdirSync(dateDir, { recursive: true });

  let wrote = 0;
  if (items.length === 0) {
    console.log("No news items returned.");
  } else {
    for (const item of items) {
      const title = pick(item, ["title", "headline", "name"]) || "Untitled";
      const id = pick(item, ["id", "uuid", "_id", "news_id"]);
      const published = toIso(
        pick(item, [
          "published_utc",
          "published_at",
          "published",
          "created_at",
          "timestamp",
        ])
      );
      const source = getSource(item);
      const summary = normalizeText(getSummary(item));
      const link = getUrl(item);
      const tickers = getTickers(item);
      const keywords = getKeywords(item);
      const insights = getInsights(item).map(normalizeText);

      if (!id) {
        continue;
      }
      const filename = safeFilename(id);
      if (!filename) continue;
      const outPath = `${dateDir}/${filename}.md`;
      if (fs.existsSync(outPath)) continue;

      const metaParts = [];
      if (published) metaParts.push(published);
      if (source) metaParts.push(source);
      if (tickers.length) metaParts.push(`tickers=${tickers.join(",")}`);
      if (keywords.length) metaParts.push(`keywords=${keywords.join(",")}`);

      const lines = [];
      lines.push(`## ${title}`);
      if (metaParts.length) lines.push(metaParts.join(" | "));
      if (summary) lines.push(summary);
      if (link) lines.push(link);
      if (insights.length) lines.push(`Insights: ${insights.join(" | ")}`);
      lines.push("");

      fs.writeFileSync(outPath, lines.join("\n"));
      wrote += 1;
    }
  }
  console.log(`Wrote ${wrote} Massive news item(s) at ${nowIso}`);
}

main().catch((err) => {
  console.error(err?.stack || String(err));
  process.exit(1);
});
