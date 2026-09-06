import fs from "fs";

// Existing game BGG IDs or normalized names in bgtags database
const EXISTING_BGG_IDS = new Set([
  315610,  // Massive Darkness 2
  342942,  // Ark Nova
  317985,  // Beyond the Sun
  124361,  // Concordia
  170216,  // Blood Rage
  312484,  // Lost Ruins of Arnak
  177736,  // A Feast for Odin
  246900,  // Eclipse: Second Dawn for the Galaxy
  229713,  // War Room
  391137,  // Galactic Cruise
  321608,  // Hegemony: Lead Your Class to Victory
  316624,  // Stationfall
  2639,    // Panzer Leader
  9823,    // Advanced Squad Leader: Starter Kit #1
  20542,   // Advanced Squad Leader: Starter Kit #3
  37111,   // Battlestar Galactica: The Board Game
  43539,   // Battlestar Galactica: The Board Game – Pegasus Expansion
  85905,   // Battlestar Galactica: The Board Game – Exodus Expansion
  141648,  // Battlestar Galactica: The Board Game – Daybreak Expansion
  243,     // Advanced Squad Leader
  5287,    // Beyond Valor: ASL Module 1
  242705,  // Aeon Trespass: Odyssey
  220308,  // Gaia Project
  1,       // Die Macher
  400602,  // Civolution
  356080,  // The Elder Scrolls: Betrayal of the Second Era
  230244,  // Black Angel
  258295,  // Ascendancy
  175155,  // Forbidden Stars
  152470,  // Fief: France 1429
  348554,  // Autobahn
  360641,  // Maladum: Dungeons of Enveron
  318182,  // Imperium: Legends
  358661,  // Andromeda's Edge
  299659,  // Clash of Cultures: Monumental Edition
  103343,  // A Game of Thrones: The Board Game (Second Edition)
  193738,  // Great Western Trail
  325494,  // ISS Vanguard
  318184,  // Imperium: Classics
  172154,  // Fief: France 1429 – Expansions Pack
  295535,  // Dark Ages: Heritage of Charlemagne
  342900,  // Earthborne Rangers
  304985,  // Dark Ages: Holy Roman Empire
  276182,  // Dead Reckoning
  364356,  // Company of Heroes: 2nd Edition
  248591,  // Dinosaur Island: Totally Liquid
  270871,  // Agemonia
  305096,  // Endless Winter: Paleoamericans
  146021,  // Eldritch Horror
  198830,  // Heroes of Land, Air & Sea
  203993,  // Lorenzo il Magnifico
  337627   // Voidfall
]);

const EXISTING_NAMES = new Set([
  "massive darkness 2",
  "ark nova",
  "beyond the sun",
  "concordia",
  "blood rage",
  "lost ruins of arnak",
  "a feast for odin",
  "eclipse: second dawn for the galaxy",
  "war room",
  "galactic cruise",
  "hegemony: lead your class to victory",
  "stationfall",
  "panzer leader",
  "advanced squad leader",
  "advanced squad leader: starter kit #1",
  "advanced squad leader: starter kit #3",
  "battlestar galactica: the board game",
  "aeon trespass: odyssey",
  "gaia project",
  "die macher",
  "civolution",
  "the elder scrolls: betrayal of the second era",
  "black angel",
  "ascendancy",
  "forbidden stars",
  "fief: france 1429",
  "autobahn",
  "maladum: dungeons of enveron",
  "imperium: legends",
  "andromeda's edge",
  "clash of cultures: monumental edition",
  "a game of thrones: the board game (second edition)",
  "great western trail",
  "iss vanguard",
  "imperium: classics",
  "dark ages: heritage of charlemagne",
  "earthborne rangers",
  "dark ages: holy roman empire",
  "dead reckoning",
  "company of heroes: 2nd edition",
  "agemonia",
  "endless winter: paleoamericans",
  "eldritch horror",
  "heroes of land, air & sea",
  "lorenzo il magnifico",
  "voidfall"
]);

async function getPageWsUrl() {
  const res = await fetch("http://localhost:9222/json");
  const pages = await res.json();
  const page = pages.find(p => p.type === "page" && p.url.includes("boardgamegeek.com"));
  return page.webSocketDebuggerUrl;
}

async function run() {
  const wsUrl = await getPageWsUrl();
  const ws = new WebSocket(wsUrl);
  await new Promise(r => ws.onopen = r);

  console.log("Fetching collection with avgweight from BGG...");
  const result = await new Promise((resolve, reject) => {
    ws.onmessage = (msg) => {
      const data = JSON.parse(msg.data);
      if (data.id === 1) {
        if (data.error) reject(data.error);
        else resolve(data.result.result.value);
        ws.close();
      }
    };

    ws.send(JSON.stringify({
      id: 1,
      method: "Runtime.evaluate",
      params: {
        expression: `(async () => {
          const url = "https://boardgamegeek.com/collection/user/Rohirrim70?objecttype=thing&ff=1&subtype=boardgame&columns%5B%5D=avgweight&columns%5B%5D=bggrating&columns%5B%5D=title&columns%5B%5D=plays";
          const r = await fetch(url);
          const html = await r.text();
          const doc = new DOMParser().parseFromString(html, "text/html");
          const rows = Array.from(doc.querySelectorAll(".collection_table tr[id^='row_']"));

          return rows.map(tr => {
            const nameEl = tr.querySelector(".collection_objectname a");
            const href = nameEl ? nameEl.getAttribute("href") : "";
            const name = nameEl ? nameEl.innerText.trim() : "";
            
            // Extract BGG ID and subtype
            const match = href.match(/\\/(boardgame(?:expansion)?)\\/(\\d+)/);
            const subtype = match ? match[1] : "boardgame";
            const bggId = match ? parseInt(match[2], 10) : null;

            // Year
            const yearSpan = tr.querySelector(".collection_objectname .smallerfont");
            const yearText = yearSpan ? yearSpan.innerText.replace(/[()]/g, "").trim() : "";
            const year = parseInt(yearText, 10) || null;

            // Geek rating
            const geekRatingTd = tr.querySelector("td.collection_bggrating");
            const geekRating = geekRatingTd ? parseFloat(geekRatingTd.innerText.trim()) || 0.0 : 0.0;

            // Average weight (complexity)
            const cells = Array.from(tr.querySelectorAll("td"));
            // Find cell with numeric float value around weight
            let weight = 0.0;
            // The th headers are: Title, Geek Rating, Avg Weight, YourPlays
            // td corresponding to avgweight is typically the 3rd or has specific class
            for (const td of cells) {
              const text = td.innerText.trim();
              if (/^\\d+\\.\\d+$/.test(text)) {
                const val = parseFloat(text);
                if (val >= 1.0 && val <= 5.0 && val !== geekRating) {
                  weight = val;
                }
              }
            }

            // Plays
            const playsTd = tr.querySelector("td.collection_plays");
            const plays = playsTd ? parseInt(playsTd.innerText.trim(), 10) || 0 : 0;

            return {
              bgg_id: bggId,
              name,
              year,
              complexity: weight,
              geek_rating: geekRating,
              plays,
              subtype,
              is_expansion: subtype === "boardgameexpansion",
              bgg_url: href ? "https://boardgamegeek.com" + href : ""
            };
          });
        })()`,
        awaitPromise: true,
        returnByValue: true
      }
    }));
  });

  console.log(`Fetched ${result.length} items from BGG collection.`);

  // Filter out existing games
  const remaining = result.filter(g => {
    if (!g.bgg_id) return false;
    if (EXISTING_BGG_IDS.has(g.bgg_id)) return false;
    const lower = g.name.toLowerCase();
    for (const exName of EXISTING_NAMES) {
      if (lower === exName || lower.startsWith(exName + " –") || lower.startsWith(exName + ":")) {
        // Keep expansions if not base game, but filter if base game
        if (!g.is_expansion && lower === exName) return false;
      }
    }
    return true;
  });

  console.log(`Remaining games after filtering: ${remaining.length}`);

  // Sort by complexity descending, then by geek rating descending
  remaining.sort((a, b) => {
    if (b.complexity !== a.complexity) {
      return b.complexity - a.complexity;
    }
    return b.geek_rating - a.geek_rating;
  });

  // Save JSON
  const jsonPath = "/home/cdk2128/Documents/projects/bgtags/remaining_games_by_complexity.json";
  fs.writeFileSync(jsonPath, JSON.stringify(remaining, null, 2));
  console.log(`Saved JSON: ${jsonPath}`);

  // Generate beautiful Markdown Table
  let md = `# Games Backlog by Complexity (Descending)\n\n`;
  md += `This list contains all games from your BoardGameGeek collection (**Rohirrim70**) that are **not yet in the \`bgtags\` database**, ordered from highest complexity (weight) to lowest.\n\n`;
  md += `Use this reference to prioritize which game rulebooks and companion guides to import next into the physical sticker and rules hub system.\n\n`;
  md += `*Total Backlog Games*: **${remaining.length}**  \n`;
  md += `*Currently Active in Database*: **52** (Aeon Trespass, Agemonia, Andromeda's Edge, Ark Nova, Autobahn, Battlestar Galactica, Black Angel, Beyond the Sun, Civolution, Clash of Cultures: Monumental Edition, Concordia, Company of Heroes 2E, Dark Ages, Dead Reckoning, Die Macher, Earthborne Rangers, Eclipse, Eldritch Horror, Endless Winter, Fief, Forbidden Stars, Gaia Project, Galactic Cruise, Great Western Trail, Hegemony, Heroes of Land Air & Sea, Imperium, ISS Vanguard, Lorenzo il Magnifico, Lost Ruins of Arnak, Maladum, Massive Darkness 2, Panzer Leader, Stationfall, The Elder Scrolls, Voidfall, War Room)\n\n`;
  md += `| # | Game Title | Year | Complexity (1-5) | Geek Rating | Plays | Type | BGG Link |\n`;
  md += `| :-: | :--- | :-: | :-: | :-: | :-: | :--- | :--- |\n`;

  remaining.forEach((g, idx) => {
    const weightStr = g.complexity > 0 ? `**${g.complexity.toFixed(2)}**` : `*N/A*`;
    const ratingStr = g.geek_rating > 0 ? g.geek_rating.toFixed(2) : `—`;
    const typeStr = g.is_expansion ? `🧩 Expansion` : `🎲 Base Game`;
    const yearStr = g.year || `—`;
    const linkStr = `[BGG ${g.bgg_id}](${g.bgg_url})`;
    md += `| ${idx + 1} | **${g.name}** | ${yearStr} | ${weightStr} | ${ratingStr} | ${g.plays} | ${typeStr} | ${linkStr} |\n`;
  });

  const mdPath = "/home/cdk2128/Documents/projects/bgtags/GAMES_TODO.md";
  fs.writeFileSync(mdPath, md);
  console.log(`Saved Markdown: ${mdPath}`);
}

run().catch(console.error);
