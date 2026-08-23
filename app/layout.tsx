import type { Metadata } from 'next';
import { Geist, Geist_Mono } from 'next/font/google';
import './globals.css';

const geistSans = Geist({
  variable: '--font-geist-sans',
  subsets: ['latin'],
});

const geistMono = Geist_Mono({
  variable: '--font-geist-mono',
  subsets: ['latin'],
});

export const metadata: Metadata = {
  metadataBase: new URL(process.env.SITE_ORIGIN ?? 'http://localhost:3000'),
  title: 'Draftside — Live Fantasy Draft Assistant',
  description: 'A live Sleeper draft companion that tracks every pick and recommends your best tactical selection.',
  openGraph: {
    title: 'Draftside',
    description: 'Your live fantasy draft copilot.',
    images: [{ url: '/og.png', width: 1672, height: 939, alt: 'Draftside — Your live fantasy draft copilot' }],
  },
  twitter: {
    card: 'summary_large_image',
    title: 'Draftside',
    description: 'Your live fantasy draft copilot.',
    images: ['/og.png'],
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased`}
      >
        {children}
      </body>
    </html>
  );
}
