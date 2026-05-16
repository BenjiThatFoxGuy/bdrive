import type { SetValue } from "@/types";
import { createLazyFileRoute } from "@tanstack/react-router";
import { Button, FieldError, InputGroup, TextField } from "@heroui/react";
import PasswordIcon from "~icons/carbon/password";
import ShowPasswordIcon from "~icons/mdi/eye-outline";
import HidePasswordIcon from "~icons/mdi/eye-off-outline";
import { useCallback, useState } from "react";
import { $api } from "@/utils/api";
import { Controller, useForm } from "react-hook-form";
import { useSessionStorage } from "usehooks-ts";
import { SharedFileBrowser } from "@/components/shared-file-browser";

const shareUnlockStorageKey = (id: string) => `share-unlocked:${id}`;

export const Route = createLazyFileRoute("/_share/share/$id")({
  component: Component,
});

function Component() {
  const { id } = Route.useParams();
  const { data: file } = $api.useSuspenseQuery("get", "/shares/{id}", {
    params: {
      path: {
        id,
      },
    },
  });

  const [shareUnlocked, setShareUnlocked] = useSessionStorage<boolean>(shareUnlockStorageKey(id), false);

  const [unlocked, setUnlocked] = useState((file.protected && shareUnlocked) || !file.protected);

  if (!unlocked) {
    return <ShareAccess id={id} setShareUnlocked={setShareUnlocked} setUnlocked={setUnlocked} />;
  }

  return !file.protected || unlocked ? <SharedFileBrowser /> : null;
}

interface ShareAccessProps {
  id: string;
  setUnlocked: SetValue<boolean>;
  setShareUnlocked: SetValue<boolean>;
}

function ShareAccess({ id, setUnlocked, setShareUnlocked }: ShareAccessProps) {
  const [showPassword, setShowPassword] = useState(false);

  const { control, handleSubmit, setError } = useForm({
    defaultValues: {
      password: "",
    },
  });

  const togglePassword = () => setShowPassword((prev) => !prev);

  const unLockMutation = $api.useMutation("post", "/shares/{id}/unlock", {
    onError: () => {
      setError("password", { message: "Invalid password" });
    },
    onSuccess: () => {
      setShareUnlocked(true);
      setUnlocked(true);
    },
  });

  const onSubmit = useCallback(async ({ password }: { password: string }) => {
    await unLockMutation.mutateAsync({ body: { password }, params: { path: { id } } });
  }, [id, unLockMutation]);

  return (
    <form
      onSubmit={handleSubmit(onSubmit)}
      className="m-auto flex rounded-large max-w-md flex-col justify-center items-center bg-surface gap-6 p-6"
    >
      <div className="size-14 bg-surface-secondary flex items-center justify-center rounded">
        <PasswordIcon className="size-8 text-surface-secondary-foreground" />
      </div>
      <h1 className="font-medium">This link is password protected</h1>

      <Controller
        name="password"
        control={control}
        rules={{ required: true }}
        render={({ field, fieldState: { error } }) => (
          <TextField
            isInvalid={Boolean(error)}
            className="max-w-xs"
            name={field.name}
            value={field.value}
            onChange={field.onChange}
          >
            <InputGroup >
              <InputGroup.Input
                ref={field.ref}
                placeholder="Enter password"
                aria-autocomplete="none"
                autoComplete="off"
                autoSave="off"
                type={showPassword ? "text" : "password"}
              />
              <InputGroup.Suffix className="pr-0">
                <Button
                  isIconOnly
                  className="size-8 min-w-8"
                  variant="ghost"
                  onPress={togglePassword}
                >
                  {showPassword ? <HidePasswordIcon /> : <ShowPasswordIcon />}
                </Button>
              </InputGroup.Suffix>
            </InputGroup>
            {error && <FieldError>{error.message}</FieldError>}
          </TextField>
        )}
      />
      <Button
        isPending={unLockMutation.isPending}
        fullWidth
        type="submit"
        variant="secondary"
        className="max-w-xs text-inherit"
      >
        Unlock
      </Button>
    </form>
  );
}
