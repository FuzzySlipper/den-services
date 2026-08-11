import { spawn, execFile } from "node:child_process";
import { once } from "node:events";
import process from "node:process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const buttonNumbers = { left: 1, middle: 2, right: 3, back: 8, forward: 9 };

export async function startVirtualDisplay({ viewport, stderr }) {
  if (process.platform !== "linux") return null;
  const width = Number(viewport?.width) || 1280;
  const height = Number(viewport?.height) || 720;
  const failures = [];
  for (let offset = 0; offset < 20; offset += 1) {
    const number = 1_000 + ((process.pid + offset) % 50_000);
    const child = spawn("Xvfb", [`:${number}`, "-screen", "0", `${width}x${height + 200}x24`, "-nolisten", "tcp"], {
      stdio: ["ignore", "ignore", "pipe"]
    });
    let errorText = "";
    child.stderr.on("data", chunk => { errorText += String(chunk); });
    const outcome = await Promise.race([
      once(child, "exit").then(([code]) => ({ exited: true, code })),
      new Promise(resolve => setTimeout(() => resolve({ exited: false }), 100))
    ]);
    if (!outcome.exited) {
      child.stderr.pipe(stderr, { end: false });
      return {
        child,
        display: `:${number}`,
        async stop() {
          if (child.exitCode !== null || child.signalCode !== null) return;
          child.kill("SIGTERM");
          await Promise.race([once(child, "exit"), new Promise(resolve => setTimeout(resolve, 1_000))]);
          if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL");
        }
      };
    }
    failures.push(`:${number} exited ${outcome.code}: ${errorText.trim()}`);
  }
  throw new Error(`unable to start Xvfb: ${failures.join("; ")}`);
}

export class VirtualMouse {
  constructor({ page, helperPath, display, viewport, exec = execFileAsync }) {
    this.page = page;
    this.helperPath = helperPath;
    this.display = display;
    this.exec = exec;
    this.position = { x: Math.floor(viewport.width / 2), y: Math.floor(viewport.height / 2) };
  }

  async move(x, y, options = {}) {
    const target = { x: Math.round(x), y: Math.round(y) };
    const steps = Math.max(1, Math.round(Number(options.steps) || 1));
    const start = this.position;
    for (let step = 1; step <= steps; step += 1) {
      const next = {
        x: Math.round(start.x + ((target.x - start.x) * step) / steps),
        y: Math.round(start.y + ((target.y - start.y) * step) / steps)
      };
      if (await this.pointerLocked()) {
        await this.command("move-relative", next.x - this.position.x, next.y - this.position.y);
      } else {
        const origin = await this.page.evaluate(() => ({
          x: window.screenX + Math.round((window.outerWidth - window.innerWidth) / 2),
          y: window.screenY + Math.round(window.outerHeight - window.innerHeight)
        }));
        await this.command("move-absolute", origin.x + next.x, origin.y + next.y);
      }
      this.position = next;
    }
  }

  async click(x, y, options = {}) {
    await this.move(x, y, options);
    const button = options.button || "left";
    const count = Math.max(1, Math.round(Number(options.clickCount) || 1));
    for (let click = 0; click < count; click += 1) {
      await this.down({ button });
      if (options.delay) await new Promise(resolve => setTimeout(resolve, Number(options.delay)));
      await this.up({ button });
    }
  }

  async down(options = {}) {
    await this.command("button", this.button(options.button), "down");
  }

  async up(options = {}) {
    await this.command("button", this.button(options.button), "up");
  }

  async pointerLocked() {
    return this.page.evaluate(() => document.pointerLockElement !== null);
  }

  button(name = "left") {
    const number = buttonNumbers[name];
    if (!number) throw new Error(`unsupported X11 mouse button ${JSON.stringify(name)}`);
    return number;
  }

  async command(...args) {
    await this.exec(this.helperPath, args.map(String), { env: { ...process.env, DISPLAY: this.display } });
  }
}
