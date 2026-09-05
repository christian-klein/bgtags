import fs from "fs";

// Helper to get active page WS URL
async function getPageWsUrl() {
  const res = await fetch("http://localhost:9222/json");
  const pages = await res.json();
  const page = pages.find(p => p.type === "page" && !p.url.startsWith("chrome://"));
  return page.webSocketDebuggerUrl;
}

function sendCommand(ws, id, method, params = {}) {
  return new Promise((resolve, reject) => {
    const handler = (msg) => {
      const data = JSON.parse(msg.data);
      if (data.id === id) {
        ws.removeEventListener("message", handler);
        if (data.error) reject(data.error);
        else resolve(data.result);
      }
    };
    ws.addEventListener("message", handler);
    ws.send(JSON.stringify({ id, method, params }));
  });
}

export async function downloadFromBgg(filePageUrl, targetPath) {
  const wsUrl = await getPageWsUrl();
  const ws = new WebSocket(wsUrl);

  await new Promise((res) => (ws.onopen = res));

  let msgId = 1;
  // Enable Page events
  await sendCommand(ws, msgId++, "Page.enable");
  await sendCommand(ws, msgId++, "Network.enable");

  console.log(`Navigating to ${filePageUrl}...`);
  await sendCommand(ws, msgId++, "Page.navigate", { url: filePageUrl });

  // Wait 3 seconds for page to load
  await new Promise((r) => setTimeout(r, 3500));

  // Find download link in page
  const evalResult = await sendCommand(ws, msgId++, "Runtime.evaluate", {
    expression: `(() => {
      const a = document.querySelector('a[href*="/file/download"]');
      return a ? a.href : null;
    })()`,
    returnByValue: true,
  });

  const downloadHref = evalResult.result.value;
  console.log("Found download link:", downloadHref);
  if (!downloadHref) {
    ws.close();
    throw new Error("Could not find download link on page");
  }

  // Listen for requestWillBeSent or responseReceived to capture S3 URL
  let s3Url = null;
  const s3Promise = new Promise((resolve) => {
    const netHandler = (msg) => {
      const data = JSON.parse(msg.data);
      if (data.method === "Network.requestWillBeSent") {
        const url = data.params.request.url;
        if (url.includes("s3.amazonaws.com/geekdo-files.com/")) {
          resolve(url);
        }
      }
    };
    ws.addEventListener("message", netHandler);
  });

  // Navigate to downloadHref
  await sendCommand(ws, msgId++, "Page.navigate", { url: downloadHref });

  s3Url = await Promise.race([
    s3Promise,
    new Promise((_, reject) =>
      setTimeout(() => reject(new Error("Timeout waiting for S3 URL")), 10000)
    ),
  ]);

  console.log("Captured fresh S3 URL:", s3Url.substring(0, 100) + "...");
  ws.close();

  // Fetch S3 URL immediately
  console.log(`Downloading to ${targetPath}...`);
  const resp = await fetch(s3Url);
  if (!resp.ok) {
    throw new Error(`Failed to download from S3: ${resp.status} ${resp.statusText}`);
  }
  const buffer = Buffer.from(await resp.arrayBuffer());
  fs.writeFileSync(targetPath, buffer);
  console.log(`Successfully saved ${targetPath} (${buffer.length} bytes)`);
}

const args = process.argv.slice(2);
if (args.length >= 2) {
  downloadFromBgg(args[0], args[1]).catch((err) => {
    console.error("Download error:", err);
    process.exit(1);
  });
}
