import type { Metadata } from "next";
import "@arco-design/web-react/dist/css/arco.css";
import "./globals.css";

export const metadata: Metadata = {
  title: "HShop",
  description: "HShop - 基于 Next.js + better-sqlite3 + Arco Design 的发卡系统",
  icons: { icon: "/favicon.svg" },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN">
      <body className="antialiased">{children}</body>
    </html>
  );
}
