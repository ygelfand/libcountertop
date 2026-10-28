import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Button, Group, Loader, Stack, Text, Textarea, TextInput } from "@mantine/core";
import { api, HTTPError, SignedOut, type KeyboardField } from "./api";

export function Keyboard({ onAuthLost }: { onAuthLost: () => void }) {
  const [field, setField] = useState<KeyboardField>();
  const [draft, setDraft] = useState("");
  const [issue, setIssue] = useState<Error>();
  const [ending, setEnding] = useState(false);
  const current = useRef<KeyboardField | undefined>(undefined);
  const wanted = useRef<string | undefined>(undefined);
  const writer = useRef<Promise<void> | undefined>(undefined);
  const timer = useRef<number | undefined>(undefined);
  const composing = useRef(false);
  const mounted = useRef(false);
  const edits = useRef(0);

  const fail = useCallback((error: unknown) => {
    if (!mounted.current) return;
    wanted.current = undefined;
    window.clearTimeout(timer.current);
    if (error instanceof SignedOut) { onAuthLost(); return; }
    current.current = undefined;
    setField(undefined);
    setDraft("");
    setIssue(error instanceof Error ? error : new Error("Connection failed"));
  }, [onAuthLost]);

  const load = useCallback(async () => {
    const version = edits.current;
    try {
      const value = await api.keyboard(AbortSignal.timeout(10000));
      if (!mounted.current || version !== edits.current || writer.current || wanted.current !== undefined || composing.current) return;
      current.current = value;
      setField(value);
      setDraft(value.text);
      setIssue(undefined);
    } catch (error) { fail(error); }
  }, [fail]);

  useEffect(() => {
    mounted.current = true;
    void load();
    let checking = false;
    const check = async () => {
      if (checking || !current.current || writer.current || wanted.current !== undefined || composing.current || document.visibilityState !== "visible") return;
      checking = true;
      await load();
      checking = false;
    };
    const poll = window.setInterval(() => void check(), 2000);
    const visible = () => { void check(); };
    document.addEventListener("visibilitychange", visible);
    return () => {
      mounted.current = false;
      window.clearInterval(poll);
      window.clearTimeout(timer.current);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [load]);

  const flush = () => {
    window.clearTimeout(timer.current);
    if (writer.current) return writer.current;
    const write = async () => {
      try {
        while (mounted.current && current.current && wanted.current !== undefined) {
          const text = wanted.current;
          wanted.current = undefined;
          const value = await api.keyboardEdit(current.current, "replace", text);
          if (!mounted.current) return;
          current.current = value;
          setField(value);
          if (wanted.current === undefined && !composing.current) setDraft(value.text);
        }
      } catch (error) { fail(error); }
    };
    const request = write().finally(() => { writer.current = undefined; });
    writer.current = request;
    return request;
  };

  const edit = (value: string, composition: boolean) => {
    edits.current++;
    setDraft(value);
    window.clearTimeout(timer.current);
    if (composition) return;
    wanted.current = value;
    timer.current = window.setTimeout(() => void flush(), 150);
  };

  const end = async (action: "save" | "cancel") => {
    setEnding(true);
    if (action === "cancel") wanted.current = undefined;
    await flush();
    if (!mounted.current) return;
    try {
      if (current.current) await api.keyboardEdit(current.current, action);
      current.current = undefined;
      setField(undefined);
      setDraft("");
      setIssue(new HTTPError(410, "Field closed"));
    } catch (error) { fail(error); }
    finally { if (mounted.current) setEnding(false); }
  };

  if (issue) return <Stack align="center">
    <Text role="alert">{issue instanceof HTTPError && issue.status === 410
      ? "Open a field on the device and scan its QR code."
      : issue instanceof HTTPError && issue.status === 409
        ? "The field changed on the device. Reload before editing."
        : "Check the connection and try again."}</Text>
    {!(issue instanceof HTTPError && issue.status === 410) && <Button onClick={() => void load()}>Try again</Button>}
  </Stack>;
  if (!field) return <Loader />;

  const input = {
    label: "Type on the device",
    value: draft,
    disabled: ending,
    autoComplete: "off",
    maxLength: 16384,
    onChange: (event: FormEvent<HTMLInputElement | HTMLTextAreaElement>) => edit(event.currentTarget.value, composing.current),
    onCompositionStart: () => { composing.current = true; wanted.current = undefined; window.clearTimeout(timer.current); },
    onCompositionEnd: (event: FormEvent<HTMLInputElement | HTMLTextAreaElement>) => { composing.current = false; edit(event.currentTarget.value, false); },
  };
  return <Stack w="100%">
    {field.password ? <TextInput {...input} type="password" /> : <Textarea {...input} autosize minRows={3} />}
    <Group justify="center">
      <Button loading={ending} disabled={composing.current} onClick={() => void end("save")}>Save</Button>
      <Button variant="light" disabled={ending} onClick={() => void end("cancel")}>Cancel</Button>
    </Group>
  </Stack>;
}
