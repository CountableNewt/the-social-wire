"use client";

import { type ReactNode, useEffect, useId, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCreateFolder } from "@/hooks/useFolders";
import { Plus } from "lucide-react";

export type CreateFolderCreatedPayload = { uri: string };

interface CreateFolderFormFieldsProps {
  /** Mirrors dialog `open` so fields reset when closed. */
  dialogOpen: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (payload: CreateFolderCreatedPayload) => void | Promise<void>;
  description?: ReactNode;
  dialogTitle?: string;
  submitLabel?: string;
  pendingSubmitLabel?: string;
}

function CreateFolderFormFields({
  dialogOpen,
  onOpenChange,
  onCreated,
  description,
  dialogTitle = "New Folder",
  submitLabel = "Create",
  pendingSubmitLabel = "Creating…",
}: CreateFolderFormFieldsProps) {
  const labelId = useId();
  const descId = `${labelId}-desc`;
  const nameId = `${labelId}-name`;
  const iconId = `${labelId}-icon`;
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("");
  const [finishing, setFinishing] = useState(false);
  const createFolder = useCreateFolder();

  useEffect(() => {
    if (!dialogOpen) {
      setName("");
      setIcon("");
    }
  }, [dialogOpen]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim() || finishing) return;
    setFinishing(true);
    try {
      const result = await createFolder.mutateAsync({
        name: name.trim(),
        icon: icon.trim() || undefined,
      });
      if (onCreated) await Promise.resolve(onCreated(result));
      onOpenChange(false);
    } catch (err) {
      console.error(err);
    } finally {
      setFinishing(false);
    }
  }

  const pending = finishing || createFolder.isPending;

  return (
    <DialogContent aria-describedby={description ? descId : undefined}>
      <DialogHeader>
        <DialogTitle>{dialogTitle}</DialogTitle>
        {description ? (
          <DialogDescription id={descId}>{description}</DialogDescription>
        ) : null}
      </DialogHeader>
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor={nameId}>Name</Label>
          <Input
            id={nameId}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Tech"
            autoFocus
            required
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={iconId}>Icon (optional)</Label>
          <Input
            id={iconId}
            value={icon}
            onChange={(e) => setIcon(e.target.value)}
            placeholder="e.g. 💻"
            maxLength={4}
          />
          <p className="text-xs text-muted-foreground">
            Enter an emoji. Leave blank to use the default folder icon.
          </p>
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            Cancel
          </Button>
          <Button type="submit" disabled={!name.trim() || pending}>
            {pending ? pendingSubmitLabel : submitLabel}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
}

export interface ControlledCreateFolderDialogProps
  extends Omit<CreateFolderFormFieldsProps, "dialogOpen"> {
  open: boolean;
}

export function ControlledCreateFolderDialog({
  open,
  onOpenChange,
  ...fields
}: ControlledCreateFolderDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <CreateFolderFormFields
        dialogOpen={open}
        onOpenChange={onOpenChange}
        {...fields}
      />
    </Dialog>
  );
}

export function NewFolderDialog() {
  const [open, setOpen] = useState(false);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          <Button variant="ghost" size="sm" className="w-full justify-start gap-2" />
        }
      >
        <Plus className="h-4 w-4" />
        New Folder
      </DialogTrigger>
      <CreateFolderFormFields dialogOpen={open} onOpenChange={setOpen} />
    </Dialog>
  );
}
