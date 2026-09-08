import { readdir, readFile } from "node:fs/promises";
import console from "node:console";
import process from "node:process";
import { URL, fileURLToPath } from "node:url";
import path from "node:path";

const source = fileURLToPath(new URL("../src/", import.meta.url));
const failures = [];
async function scan(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const file = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      await scan(file);
    } else if (/\.(tsx|css)$/.test(entry.name) && !/\.test\./.test(entry.name)) {
      const lines = (await readFile(file, "utf8")).split("\n");
      lines.forEach((line, index) => {
        // Match UI utility colors, not merchant-entered color values or image assets.
        const privatePalette = /(?:bg|text|border|ring|accent|from|via|to|shadow)-(?:indigo|violet|purple|fuchsia)-\d/;
        const privateColor = /(?:bg|text|border|ring|divide|outline)-\[#[\da-fA-F]{3,8}\]/;
        if (privatePalette.test(line) || privateColor.test(line)) {
          failures.push(`${path.relative(source, file)}:${index + 1}: use a semantic UI color from index.css`);
        }
      });
    }
  }
}
await scan(source);
if (failures.length) {
  console.error(failures.join("\n"));
  process.exitCode = 1;
} else {
  console.log("Design colors: shared semantic color references checked.");
}
