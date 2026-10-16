PROGRAM AliasSt
(* A start/stop seal-in on aliased tags: the program names the tags. *)
VAR_EXTERNAL
    StartPB : BOOL;
    StopPB  : BOOL;
    st_Run  : BOOL;
END_VAR

st_Run := (StartPB OR st_Run) AND NOT StopPB;
END_PROGRAM
