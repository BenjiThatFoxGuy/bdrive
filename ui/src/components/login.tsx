import { useQuery } from "@tanstack/react-query";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import {
  Button,
  FieldError,
  Input,
  InputGroup,
  Label,
  Spinner,
  TextField,
} from "@heroui/react";
import { AsYouType, type CountryCode, getPhoneCode } from "libphonenumber-js";
import meta from "libphonenumber-js/metadata.min.json";
import { Controller, useForm } from "react-hook-form";
import toast from "react-hot-toast";

import QrCode from "@/components/qr-code";
import { TelegramIcon } from "@/components/telegram-icon";

import { PhoneNoPicker } from "./menus/phone-picker";
import { getCountryCode } from "@/utils/common";
import { $api, fetchClient } from "@/utils/api";

interface AuthAttemptSession {
  name: string;
  userName: string;
  userId: number;
  isPremium: boolean;
  session: string;
}

const getKeys = Object.keys as <T>(object: T) => (keyof T)[];

export const displayNames = new Intl.DisplayNames(["en"], { type: "region" });

function sortISOCodes(countryCodes: CountryCode[]) {
  return [...countryCodes].toSorted((countryCodeA, countryCodeB) => {
    const countryA = displayNames.of(countryCodeA) as string;
    const countryB = displayNames.of(countryCodeB) as string;

    return countryA.localeCompare(countryB);
  });
}

export const isoCodes = sortISOCodes(getKeys(meta.countries))
  .filter((x) => x !== "TA" && x !== "AC")
  .map((code) => ({
    id: code,
    code,
    country: displayNames.of(code) as string,
    value: `+${getPhoneCode(code)}`,
  }));

export const isoCodeMap = isoCodes.reduce(
  (acc, value) => {
    acc[value.code] = value;
    return acc;
  },
  {} as Record<CountryCode, (typeof isoCodes)[0]>,
);

function getTypedNumber(value: string, defaultCountryCode = "IN") {
  if (value) {
    const phone = new AsYouType(defaultCountryCode as CountryCode);
    phone.input(value);
    return phone
      .getNumber()
      ?.formatInternational()
      .replace(isoCodeMap[defaultCountryCode].value, "");
  }
  return value;
}

export interface FormState {
  otpCodeHash?: string;
  otpCode: string;
  phoneNumber: string;
  phoneCode: CountryCode;
  password?: string;
}

type LoginType = "qr" | "phone";

interface AuthAttemptSnapshot {
  id: string;
  authType?: LoginType;
  state: "created" | "qr_pending" | "code_sent" | "password_required" | "authenticated" | "failed" | "expired";
  token?: string;
  phoneCodeHash?: string;
  session?: AuthAttemptSession;
  message?: string;
}

interface StoredAttempt {
  id: string;
  loginType: LoginType;
  phoneNumber?: string;
  phoneCode?: CountryCode;
}

interface ActiveAttempt {
  id: string;
  stored?: StoredAttempt | null;
}

const authAttemptStorageKey = "teldrive.auth.attempt";

function readStoredAttempt(): StoredAttempt | null {
  try {
    const raw = window.sessionStorage.getItem(authAttemptStorageKey);
    if (!raw) {return null;}
    return JSON.parse(raw) as StoredAttempt;
  } catch {
    return null;
  }
}

function writeStoredAttempt(value: StoredAttempt) {
  window.sessionStorage.setItem(authAttemptStorageKey, JSON.stringify(value));
}

function clearStoredAttempt() {
  window.sessionStorage.removeItem(authAttemptStorageKey);
}

const initialState = {
  form: {
    phoneCode: getCountryCode(),
    phoneNumber: "",
  } as FormState,
  isLoading: false,
  loginType: "phone" as LoginType,
  qrCode: "",
  step: 1,
};

export const Login = memo(() => {
  const { redirect } = useSearch({ from: "/_auth/login" });

  const [state, setState] = useState(initialState);

  const { control, handleSubmit, getValues, setError } = useForm({
    defaultValues: initialState.form,
  });

  const attemptIdRef = useRef<string | null>(null);
  const loginAttemptRef = useRef<string | null>(null);
  const [activeAttempt, setActiveAttempt] = useState<ActiveAttempt | null>(null);

  const { mutateAsync: submitLogin } = $api.useMutation("post", "/auth/login", {});

  const applyAttemptSnapshot = useCallback((attempt: AuthAttemptSnapshot, stored?: StoredAttempt | null) => {
    if (attempt.token) {
      setState((prev) => ({ ...prev, qrCode: attempt.token || "" }));
    }
    if (attempt.state === "code_sent" && attempt.phoneCodeHash) {
      setState((prev) => ({
        ...prev,
        form: {
          ...prev.form,
          otpCodeHash: attempt.phoneCodeHash,
          phoneCode: stored?.phoneCode || prev.form.phoneCode,
          phoneNumber: stored?.phoneNumber || prev.form.phoneNumber,
        },
        isLoading: false,
        step: 2,
      }));
    }
    if (attempt.state === "password_required") {
      setState((prev) => ({
        ...prev,
        form: {
          ...prev.form,
          phoneCode: stored?.phoneCode || prev.form.phoneCode,
          phoneNumber: stored?.phoneNumber || prev.form.phoneNumber,
        },
        isLoading: false,
        step: 3,
      }));
    }
    if (attempt.state === "authenticated" && attempt.session) {
      if (loginAttemptRef.current === attempt.id) {
        return;
      }
      loginAttemptRef.current = attempt.id;
      setActiveAttempt(null);
      submitLogin({ body: attempt.session as never }).finally(() => {
        // Window.location.replace(new URL(redirect || "/", window.location.origin));
      });
      return;
    }
    if (attempt.state === "failed") {
      setActiveAttempt(null);
      setState((prev) => ({ ...prev, isLoading: false }));
      if (attempt.message === "PHONE_CODE_INVALID") {
        setError("otpCode", { message: "Invalid OTP Code" });
        return;
      }
      toast.error(attempt.message || "Authentication failed");
      return;
    }
    if (attempt.state === "expired") {
      setActiveAttempt(null);
      setState((prev) => ({
        ...prev,
        isLoading: false,
      }));
      toast.error(attempt.message || "Login attempt expired");
    }
  }, [redirect, setError, submitLogin]);

  const attemptQuery = useQuery({
    enabled: Boolean(activeAttempt?.id),
    queryFn: async () => {
      const { data, error } = await fetchClient.GET("/auth/attempts/{id}", {
        params: { path: { id: activeAttempt!.id } },
      });
      if (error || !data) {
        throw error || new Error("Failed to fetch auth attempt");
      }
      return data as AuthAttemptSnapshot;
    },
    queryKey: ["auth-attempt", activeAttempt?.id],
    refetchInterval: (query) => {
      const attempt = query.state.data as AuthAttemptSnapshot | undefined;
      if (!activeAttempt?.id) {return false;}
      if (!attempt) {return 1000;}
      return ["authenticated", "failed", "expired"].includes(attempt.state) ? false : 1000;
    },
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    retry: false,
  });

  useEffect(() => {
    if (!attemptQuery.error || !activeAttempt?.id) {
      return;
    }
    setActiveAttempt(null);
    setState((prev) => ({ ...prev, isLoading: false }));
  }, [attemptQuery.error, activeAttempt?.id]);

  useEffect(() => {
    if (!attemptQuery.data || !activeAttempt?.id) {
      return;
    }
    if (attemptIdRef.current !== activeAttempt.id) {
      return;
    }
    applyAttemptSnapshot(attemptQuery.data, activeAttempt.stored ?? readStoredAttempt());
  }, [attemptQuery.data, activeAttempt, applyAttemptSnapshot]);

  const cleanupAttempt = useCallback(async (attemptId?: string | null) => {
    setActiveAttempt(null);
    const id = attemptId || attemptIdRef.current;
    attemptIdRef.current = null;
    loginAttemptRef.current = null;
    clearStoredAttempt();
    if (!id) {return;}
    await fetchClient.DELETE("/auth/attempts/{id}", {
      params: { path: { id } },
    }).catch(() => undefined);
  }, []);

  useEffect(() => {
    let disposed = false;
    const previousAttemptId = attemptIdRef.current;
    attemptIdRef.current = null;
    loginAttemptRef.current = null;
    setActiveAttempt(null);
    setState((prev) => ({
      ...prev,
      form: { ...prev.form, otpCode: "", otpCodeHash: undefined, password: undefined },
      isLoading: false,
      qrCode: "",
      step: 1,
    }));

    const startAttempt = async () => {
      const stored = readStoredAttempt();
      if (stored && stored.loginType === state.loginType) {
        const { data, error } = await fetchClient.GET("/auth/attempts/{id}", {
          params: { path: { id: stored.id } },
        });
        if (!disposed && !error && data) {
          attemptIdRef.current = data.id;
          setActiveAttempt({ id: data.id, stored });
          setState((prev) => ({
            ...prev,
            form: {
              ...prev.form,
              phoneCode: stored.phoneCode || prev.form.phoneCode,
              phoneNumber: stored.phoneNumber || prev.form.phoneNumber,
            },
          }));
          applyAttemptSnapshot(data as AuthAttemptSnapshot, stored);
          return;
        }
        clearStoredAttempt();
      }
      if (previousAttemptId) {
        await fetchClient.DELETE("/auth/attempts/{id}", {
          params: { path: { id: previousAttemptId } },
        }).catch(() => undefined);
      }
      if (state.loginType !== "qr") {
        return;
      }
      const { data, error } = await fetchClient.POST("/auth/attempts", {
        body: { authType: state.loginType },
      });
      if (disposed) {
        if (data?.id) {
          await fetchClient.DELETE("/auth/attempts/{id}", {
            params: { path: { id: data.id } },
          }).catch(() => undefined);
        }
        return;
      }
      if (error || !data) {
        toast.error("Failed to initialize login");
        return;
      }
      attemptIdRef.current = data.id;
      writeStoredAttempt({ id: data.id, loginType: state.loginType });
      setActiveAttempt({ id: data.id, stored: { id: data.id, loginType: state.loginType } });
      applyAttemptSnapshot(data as AuthAttemptSnapshot, { id: data.id, loginType: state.loginType });
    };

    startAttempt();
    return () => {
      disposed = true;
    };
  }, [applyAttemptSnapshot, state.loginType]);

  useEffect(() => () => {
    cleanupAttempt();
  }, [cleanupAttempt]);

  const onSubmit = useCallback(
    async ({ phoneNumber, otpCode, password, phoneCode }: FormState) => {
      if (state.step === 1 && state.loginType === "phone") {
        setState((prev) => ({
          ...prev,
          form: { ...prev.form, phoneCode, phoneNumber },
          isLoading: true,
        }));
        await cleanupAttempt();
        const { data, error } = await fetchClient.POST("/auth/attempts", {
          body: {
            authType: "phone",
            phoneNo: `+${getPhoneCode(phoneCode)}${phoneNumber}`,
          },
        });
        if (error || !data) {
          setState((prev) => ({ ...prev, isLoading: false }));
          toast.error("Failed to send code");
          return;
        }
        attemptIdRef.current = data.id;
        const stored = { id: data.id, loginType: "phone" as const, phoneCode, phoneNumber };
        writeStoredAttempt(stored);
        setActiveAttempt({ id: data.id, stored });
        applyAttemptSnapshot(data as AuthAttemptSnapshot, stored);
      } else if (state.step === 2 && state.loginType === "phone") {
        const attemptId = attemptIdRef.current;
        if (!attemptId) {
          toast.error("Login attempt not ready");
          return;
        }
        setState((prev) => ({
          ...prev,
          isLoading: true,
        }));
        await fetchClient.POST("/auth/attempts/{id}/phone/sign-in", {
          body: {
            phoneCode: otpCode,
            phoneCodeHash: state.form.otpCodeHash || "",
            phoneNo: `+${getPhoneCode(phoneCode)}${phoneNumber}`,
          },
          params: { path: { id: attemptId } },
        }).catch(() => {
          setState((prev) => ({ ...prev, isLoading: false }));
          toast.error("Failed to sign in");
        });
      } else if (state.step === 3) {
        const attemptId = attemptIdRef.current;
        if (!attemptId) {
          toast.error("Login attempt not ready");
          return;
        }
        setState((prev) => ({
          ...prev,
          isLoading: true,
        }));
        await fetchClient.POST("/auth/attempts/{id}/password", {
          body: { password: password || "" },
          params: { path: { id: attemptId } },
        }).catch(() => {
          setState((prev) => ({ ...prev, isLoading: false }));
          toast.error("Failed to verify password");
        });
      }
    },
    [applyAttemptSnapshot, cleanupAttempt, state.form.otpCodeHash, state.loginType, state.step],
  );

  const onInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const value = getTypedNumber(e.target.value, getValues("phoneCode"));
    e.target.value = value || "";
  }, []);

  useEffect(() => {
    const attemptId = attemptIdRef.current;
    if (!attemptId || state.loginType !== "phone") {return;}
    const stored = readStoredAttempt();
    if (!stored || stored.id !== attemptId) {return;}
    writeStoredAttempt({
      ...stored,
      phoneCode: state.form.phoneCode,
      phoneNumber: state.form.phoneNumber,
    });
  }, [state.form.phoneCode, state.form.phoneNumber, state.loginType]);

  return (
    <div className="mx-auto mt-12 flex w-full max-w-sm flex-col items-center">
      <form
        autoComplete="off"
        className="flex w-full flex-col items-center gap-6 rounded-2xl border border-border bg-surface p-6 pt-8"
        onSubmit={handleSubmit(onSubmit)}
      >
        <div className="flex flex-col items-center gap-2">
          <TelegramIcon className="size-14 text-muted" />
          <h1 className="text-xl font-semibold">Sign in to TelDrive</h1>
          <p className="text-sm text-muted">Enter your Telegram account details</p>
        </div>

        {state.loginType === "phone" ? (
          <div className="flex w-full flex-col gap-4">
            {state.step === 1 && (
              <Controller
                name="phoneNumber"
                control={control}
                rules={{ required: true }}
                render={({ field }) => (
                  <TextField isRequired className="w-full" name="phoneNumber">
                    <Label>Phone Number</Label>
                    <InputGroup variant="secondary">
                      <InputGroup.Prefix>
                        <Controller
                          name="phoneCode"
                          control={control}
                          render={({ field: phoneCodeField }) => (
                            <PhoneNoPicker field={phoneCodeField} />
                          )}
                        />
                      </InputGroup.Prefix>
                      <InputGroup.Input
                        placeholder="Phone Number"
                        {...field}
                        onChange={(e) => {
                          onInputChange(e);
                          field.onChange(e);
                        }}
                      />
                    </InputGroup>
                    <FieldError />
                  </TextField>
                )}
              />
            )}
            {state.step === 2 && (
              <Controller
                name="otpCode"
                control={control}
                rules={{ required: true }}
                render={({ field }) => (
                  <TextField isRequired className="w-full" name="otpCode">
                    <Label>OTP Code</Label>
                    <Input variant="secondary" placeholder="Enter the code" {...field} />
                    <FieldError />
                  </TextField>
                )}
              />
            )}
            {state.step === 3 && (
              <Controller
                name="password"
                control={control}
                rules={{ required: true }}
                render={({ field }) => (
                  <TextField isRequired className="w-full" name="password">
                    <Label>2FA Password</Label>
                    <Input variant="secondary" placeholder="Enter your 2FA password" type="password" {...field} />
                    <FieldError />
                  </TextField>
                )}
              />
            )}
          </div>
        ) : (
          <div className="grid min-h-48 w-full place-content-center">
            {state.step !== 3 && state.qrCode && <QrCode qrCode={state.qrCode} />}
            {state.step !== 3 && !state.qrCode && <Spinner className="size-10" />}
          </div>
        )}

        <div className="flex w-full flex-col items-center gap-2">
          {(state.loginType === "phone" || state.step === 3) && (
            <Button
              type="submit"
              fullWidth
              variant="primary"
              isPending={state.isLoading}
            >
              {state.isLoading ? "Please Wait\u2026" : (state.step === 1 ? "Next" : "Login")}
            </Button>
          )}
          {state.step !== 3 && (
            <Button
              onPress={() =>
                setState((prev) => ({
                  ...prev,
                  loginType: prev.loginType === "qr" ? "phone" : "qr",
                }))
              }
              fullWidth
              variant="ghost"
            >
              {state.loginType === "qr" ? "Phone Login" : "QR Login"}
            </Button>
          )}
        </div>
      </form>
    </div>
  );
});
