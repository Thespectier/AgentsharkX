import { describe, expect, it } from "vitest";

import { commandPaletteItems } from "./command-palette";

describe("commandPaletteItems", () => {
  it("exposes Demo Lab only when the management status explicitly enables it", () => {
    expect(commandPaletteItems(false).map((item) => item.label)).not.toContain("Open Demo Lab");
    expect(commandPaletteItems(true).map((item) => item.label)).toContain("Open Demo Lab");
  });
});
