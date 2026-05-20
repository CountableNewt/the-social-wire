"use client";

import { useCallback, useState } from "react";
import { Dialog } from "@/components/ui/dialog";
import { CreateFolderFormFields } from "./CreateFolderFormFields";
import type { ControlledCreateFolderDialogProps } from "./types";

export function ControlledCreateFolderDialog({
  open,
  onOpenChange,
  ...fields
}: ControlledCreateFolderDialogProps) {
  const [formKey, setFormKey] = useState(0);

  const handleOpenChange = useCallback(
    (next: boolean) => {
      onOpenChange(next);
      if (!next) setFormKey((k) => k + 1);
    },
    [onOpenChange]
  );

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <CreateFolderFormFields key={formKey} onOpenChange={handleOpenChange} {...fields} />
    </Dialog>
  );
}
