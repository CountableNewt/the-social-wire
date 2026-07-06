"use client";

import { useCallback, useState } from "react";
import { Button } from "@/components/ui/button";
import { SIDEBAR_GLASS_ROW_ACTION } from "@/components/ui/sidebar";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Plus } from "lucide-react";
import { cn } from "@/lib/utils";
import { CreateFolderFormFields } from "./NewFolder/CreateFolderFormFields";

export { ControlledCreateFolderDialog } from "./NewFolder/ControlledCreateFolderDialog";
export type {
  CreateFolderCreatedPayload,
  CreateFolderFormFieldsProps,
  ControlledCreateFolderDialogProps,
} from "./NewFolder/types";

export function NewFolderDialog() {
  const [open, setOpen] = useState(false);
  const [formKey, setFormKey] = useState(0);

  const handleOpenChange = useCallback((next: boolean) => {
    setOpen(next);
    if (next) setFormKey((k) => k + 1);
  }, []);

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button
            variant="ghost"
            className={cn(SIDEBAR_GLASS_ROW_ACTION, "px-2")}
          />
        }
      >
        <Plus className="h-4 w-4" />
        New Folder
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New Folder</DialogTitle>
        </DialogHeader>
        <CreateFolderFormFields
          key={formKey}
          onCloseRequest={() => handleOpenChange(false)}
        />
      </DialogContent>
    </Dialog>
  );
}
