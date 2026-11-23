import { useEffect, useId, useMemo, useState, type CSSProperties } from "react";
import { LANGS, useLang, useT, type Key } from "../i18n";
import {
  COLOR_KEY_LIST, contrastRatio, deleteCustom, exportTheme, FONT_MONO, FONT_UI, importTheme, PRESETS, saveCustom, setChoiceFor, slug, themeVars, toHex6,
  type ColorKey, type Theme,
} from "../theme/engine";
import { usePrefs } from "../prefs/PrefsProvider";
import { useTheme } from "../theme/useTheme";
import styles from "./Page.module.css";
import editor from "./Appearance.module.css";

export function AppearancePage() {
  const t = useT();
  const { choice, custom, setChoice } = useTheme();
  const [lang, setLang] = useLang();
  const { units, temperature, setUnits, setTemperature } = usePrefs();
  const [draft, setDraft] = useState<Theme | null>(null);
  useEffect(() => { document.title = `${t("app.title")} · Yubeos`; }, [t]);

  const current: Theme | undefined = useMemo(
    () => (choice.startsWith("custom:") ? custom.find((c) => c.id === choice.slice(7)) : PRESETS.find((p) => p.id === choice)) ?? PRESETS[0],
    [choice, custom],
  );

  const startFrom = (base: Theme, fresh: boolean) =>
    setDraft(structuredClone({ ...base, id: fresh ? slug(`${base.name} custom`) : base.id, name: fresh ? `${base.name} custom` : base.name }));

  return (
    <div className={styles.page}>
      <h1>{t("app.title")}</h1>
      <p className={styles.hint}>{t("app.intro")}</p>

      <section className={styles.card} aria-label={t("app.theme")}>
        <h2>{t("app.theme")}</h2>
        <div className={styles.field}>
          <label htmlFor="theme-select">{t("app.theme")}</label>
          <select id="theme-select" value={choice} onChange={(e) => setChoice(e.target.value)}>
            <option value="system">{t("shell.themeSystem")}</option>
            <optgroup label={t("app.presets")}>{PRESETS.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</optgroup>
            {custom.length > 0 && <optgroup label={t("app.custom")}>{custom.map((c) => <option key={c.id} value={`custom:${c.id}`}>{c.name}</option>)}</optgroup>}
          </select>
        </div>
        <div className={styles.row}>
          {current && <button type="button" className={styles.secondary} onClick={() => startFrom(current, !choice.startsWith("custom:"))}>{t("app.customize")}</button>}
          {choice.startsWith("custom:") && current && (
            <>
              <button type="button" className={styles.secondary} onClick={() => download(current)}>{t("app.export")}</button>
              <button type="button" className={styles.danger} onClick={() => deleteCustom(current.id)}>{t("app.delete")}</button>
            </>
          )}
        </div>
        <div className={styles.field}>
          <label htmlFor="lang-select">{t("app.language")}</label>
          <select id="lang-select" value={lang} onChange={(e) => setLang(e.target.value as "en" | "it")}>
            {LANGS.map((l) => <option key={l.id} value={l.id}>{l.label}</option>)}
          </select>
        </div>
        <div className={styles.field}>
          <label htmlFor="units-select">{t("app.units")}</label>
          <select id="units-select" value={units} onChange={(e) => setUnits(e.target.value as "bytes" | "bits")}>
            <option value="bytes">{t("app.unitsBytes")}</option>
            <option value="bits">{t("app.unitsBits")}</option>
          </select>
        </div>
        <div className={styles.field}>
          <label htmlFor="temp-select">{t("app.temperature")}</label>
          <select id="temp-select" value={temperature} onChange={(e) => setTemperature(e.target.value as "c" | "f")}>
            <option value="c">{t("app.tempC")}</option>
            <option value="f">{t("app.tempF")}</option>
          </select>
        </div>
      </section>

      {draft && <Editor draft={draft} setDraft={setDraft} onClose={() => setDraft(null)} onSaved={(th) => { setChoiceFor(th); setDraft(null); }} />}
      <ImportCard />
    </div>
  );
}

function download(th: Theme) {
  const url = URL.createObjectURL(new Blob([exportTheme(th)], { type: "application/json" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = `${th.id}.yubeos-theme.json`;
  a.click();
  URL.revokeObjectURL(url);
}

function Editor({ draft, setDraft, onClose, onSaved }: { draft: Theme; setDraft: (t: Theme) => void; onClose: () => void; onSaved: (t: Theme) => void }) {
  const t = useT();
  const uid = useId();
  const setColor = (k: ColorKey, v: string) => setDraft({ ...draft, colors: { ...draft.colors, [k]: v } });
  const radius = draft.shape?.radiusPx ?? 8;
  const size = draft.typography.baseSizePx ?? 14;
  const lh = draft.typography.lineHeight ?? 1.5;
  const warnings = ([["text", "bg"], ["textMuted", "bg"], ["accentContrast", "accent"]] as [ColorKey, ColorKey][])
    .map(([a, b]) => ({ a, b, ratio: contrastRatio(draft.colors[a], draft.colors[b]) }))
    .filter((w) => w.ratio < 4.5);

  return (
    <section className={styles.card} aria-label={t("app.editor")}>
      <h2>{t("app.editor")}</h2>
      <div className={styles.field}>
        <label htmlFor={`${uid}-n`}>{t("app.name")}</label>
        <input id={`${uid}-n`} maxLength={60} value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
      </div>
      <div className={styles.field}>
        <label htmlFor={`${uid}-b`}>{t("app.base")}</label>
        <select id={`${uid}-b`} value={draft.base} onChange={(e) => setDraft({ ...draft, base: e.target.value as Theme["base"] })}>
          <option value="dark">{t("app.baseDark")}</option>
          <option value="light">{t("app.baseLight")}</option>
        </select>
      </div>

      <h3>{t("app.colors")}</h3>
      <div className={editor.colors}>
        {COLOR_KEY_LIST.map((k) => (
          <label key={k} className={editor.color}>
            <input type="color" value={toHex6(draft.colors[k])} onChange={(e) => setColor(k, e.target.value)} />
            <span>{t(`color.${k}` as Key)}</span>
            <code>{toHex6(draft.colors[k])}</code>
          </label>
        ))}
      </div>
      {warnings.map((w) => (
        <p key={w.a + w.b} className={styles.error} role="status">
          {t("app.contrastLow", { a: t(`color.${w.a}` as Key), b: t(`color.${w.b}` as Key), ratio: w.ratio.toFixed(1) })}
        </p>
      ))}

      <h3>{t("app.typography")}</h3>
      <div className={styles.field}>
        <label htmlFor={`${uid}-fu`}>{t("app.fontUi")}</label>
        <select id={`${uid}-fu`} value={draft.typography.ui} onChange={(e) => setDraft({ ...draft, typography: { ...draft.typography, ui: e.target.value } })}>
          {FONT_UI.map((f) => <option key={f.id} value={f.value}>{f.label}</option>)}
          {!FONT_UI.some((f) => f.value === draft.typography.ui) && <option value={draft.typography.ui}>{draft.typography.ui}</option>}
        </select>
      </div>
      <div className={styles.field}>
        <label htmlFor={`${uid}-fm`}>{t("app.fontMono")}</label>
        <select id={`${uid}-fm`} value={draft.typography.mono} onChange={(e) => setDraft({ ...draft, typography: { ...draft.typography, mono: e.target.value } })}>
          {FONT_MONO.map((f) => <option key={f.id} value={f.value}>{f.label}</option>)}
          {!FONT_MONO.some((f) => f.value === draft.typography.mono) && <option value={draft.typography.mono}>{draft.typography.mono}</option>}
        </select>
      </div>
      <div className={styles.field}>
        <label htmlFor={`${uid}-s`}>{t("app.fontSize", { n: size })}</label>
        <input id={`${uid}-s`} type="range" min={12} max={18} step={1} value={size} onChange={(e) => setDraft({ ...draft, typography: { ...draft.typography, baseSizePx: Number(e.target.value) } })} />
      </div>
      <div className={styles.field}>
        <label htmlFor={`${uid}-l`}>{t("app.lineHeight", { n: lh })}</label>
        <input id={`${uid}-l`} type="range" min={1.2} max={2} step={0.1} value={lh} onChange={(e) => setDraft({ ...draft, typography: { ...draft.typography, lineHeight: Math.round(Number(e.target.value) * 10) / 10 } })} />
      </div>

      <h3>{t("app.shape")}</h3>
      <div className={styles.field}>
        <label htmlFor={`${uid}-r`}>{t("app.radius", { n: radius })}</label>
        <input id={`${uid}-r`} type="range" min={0} max={16} step={1} value={radius} onChange={(e) => setDraft({ ...draft, shape: { ...draft.shape, radiusPx: Number(e.target.value) } })} />
      </div>
      <div className={styles.field}>
        <label htmlFor={`${uid}-d`}>{t("app.density")}</label>
        <select id={`${uid}-d`} value={draft.shape?.density ?? "comfortable"} onChange={(e) => setDraft({ ...draft, shape: { ...draft.shape, density: e.target.value as "compact" } })}>
          <option value="compact">{t("app.densityCompact")}</option>
          <option value="comfortable">{t("app.densityComfortable")}</option>
          <option value="spacious">{t("app.densitySpacious")}</option>
        </select>
      </div>

      <h3>{t("app.preview")}</h3>
      <div className={editor.preview} style={themeVars(draft) as CSSProperties} data-testid="theme-preview">
        <strong>{draft.name}</strong>
        <p>{t("app.previewText")}</p>
        <button type="button" className={editor.previewButton}>{t("app.previewButton")}</button>
      </div>

      <div className={styles.row}>
        <button type="button" className={styles.button} disabled={!draft.name.trim()} onClick={() => { const th = { ...draft, id: draft.id || slug(draft.name) }; saveCustom(th); onSaved(th); }}>{t("app.save")}</button>
        <button type="button" className={styles.secondary} onClick={onClose}>{t("app.cancel")}</button>
      </div>
    </section>
  );
}

function ImportCard() {
  const t = useT();
  const uid = useId();
  const [text, setText] = useState("");
  const [result, setResult] = useState<{ ok: boolean; lines: string[] } | null>(null);

  const apply = (raw: string) => {
    const r = importTheme(raw);
    if (!r.ok) return setResult({ ok: false, lines: r.errors });
    saveCustom(r.theme);
    setChoiceFor(r.theme);
    setResult({ ok: true, lines: [t("app.importOk")] });
    setText("");
  };

  return (
    <section className={styles.card} aria-label={t("app.import")}>
      <h2>{t("app.import")}</h2>
      <p className={styles.hint}>{t("app.importHint")}</p>
      {result && (
        <div className={result.ok ? styles.success : styles.error} role="alert">
          {!result.ok && <b>{t("app.invalid")}</b>}
          <ul>{result.lines.map((l) => <li key={l}>{l}</li>)}</ul>
        </div>
      )}
      <div className={styles.field}>
        <label htmlFor={`${uid}-t`}>JSON</label>
        <textarea id={`${uid}-t`} rows={5} spellCheck={false} value={text} onChange={(e) => setText(e.target.value)} />
      </div>
      <div className={styles.row}>
        <label className={styles.secondary}>
          {t("app.importFile")}
          <input type="file" accept="application/json,.json" hidden onChange={(e) => { const f = e.target.files?.[0]; if (f) void f.text().then(apply); }} />
        </label>
        <button type="button" className={styles.button} disabled={!text.trim()} onClick={() => apply(text)}>{t("app.importApply")}</button>
      </div>
    </section>
  );
}
