(* psu_lab.st: a synthetic fixture for the supply blocks of lib/server.st:
   LAB1 with one supply (PsuSingle) and LAB4 with four (PsuUnit), wired as
   gen.py wires a server. Hand-written, not generated; none of it is part
   of the recorded cluster. The tests are in it-cluster_test.yaml. *)
PROGRAM PsuLab
VAR_EXTERNAL
  Sim_Room : SimRoom;
  LAB1 : Server; Sim_LAB1 : SimServer;
  LAB1_PSU1 : PSU; Sim_LAB1_PSU1 : SimPSU;
  LAB4 : Server; Sim_LAB4 : SimServer;
  LAB4_PSU1 : PSU; Sim_LAB4_PSU1 : SimPSU;
  LAB4_PSU2 : PSU; Sim_LAB4_PSU2 : SimPSU;
  LAB4_PSU3 : PSU; Sim_LAB4_PSU3 : SimPSU;
  LAB4_PSU4 : PSU; Sim_LAB4_PSU4 : SimPSU;
END_VAR
VAR
  LAB1_M : ServerPlant;
  LAB1_Psu_M : PsuSingle;
  LAB4_M : ServerPlant;
  LAB4_PsuUp : DINT;
  LAB4_PSU1_M : PsuUnit;
  LAB4_PSU2_M : PsuUnit;
  LAB4_PSU3_M : PsuUnit;
  LAB4_PSU4_M : PsuUnit;
END_VAR
LAB1_M(Rec := LAB1, F := Sim_LAB1, Room := Sim_Room, FansFailed := 0, CpuRiseNow := 0.0, Live := FALSE,
  Unpowered := Sim_LAB1_PSU1.InputLost OR Sim_LAB1_PSU1.Fail, P := LAB1);
LAB1_Psu_M(Rec := LAB1_PSU1, F := Sim_LAB1_PSU1, LoadW := LAB1_M.LoadW, Live := FALSE, P := LAB1_PSU1);
LAB4_M(Rec := LAB4, F := Sim_LAB4, Room := Sim_Room, FansFailed := 0, CpuRiseNow := 0.0, Live := FALSE,
  Unpowered := (Sim_LAB4_PSU1.InputLost OR Sim_LAB4_PSU1.Fail) AND (Sim_LAB4_PSU2.InputLost OR Sim_LAB4_PSU2.Fail)
    AND (Sim_LAB4_PSU3.InputLost OR Sim_LAB4_PSU3.Fail) AND (Sim_LAB4_PSU4.InputLost OR Sim_LAB4_PSU4.Fail), P := LAB4);
LAB4_PsuUp := BOOL_TO_INT(NOT (Sim_LAB4_PSU1.InputLost OR Sim_LAB4_PSU1.Fail)) + BOOL_TO_INT(NOT (Sim_LAB4_PSU2.InputLost OR Sim_LAB4_PSU2.Fail))
  + BOOL_TO_INT(NOT (Sim_LAB4_PSU3.InputLost OR Sim_LAB4_PSU3.Fail)) + BOOL_TO_INT(NOT (Sim_LAB4_PSU4.InputLost OR Sim_LAB4_PSU4.Fail));
LAB4_PSU1_M(Rec := LAB4_PSU1, F := Sim_LAB4_PSU1, LoadW := LAB4_M.LoadW, UpCount := LAB4_PsuUp, Live := FALSE, P := LAB4_PSU1);
LAB4_PSU2_M(Rec := LAB4_PSU2, F := Sim_LAB4_PSU2, LoadW := LAB4_M.LoadW, UpCount := LAB4_PsuUp, Live := FALSE, P := LAB4_PSU2);
LAB4_PSU3_M(Rec := LAB4_PSU3, F := Sim_LAB4_PSU3, LoadW := LAB4_M.LoadW, UpCount := LAB4_PsuUp, Live := FALSE, P := LAB4_PSU3);
LAB4_PSU4_M(Rec := LAB4_PSU4, F := Sim_LAB4_PSU4, LoadW := LAB4_M.LoadW, UpCount := LAB4_PsuUp, Live := FALSE, P := LAB4_PSU4);
END_PROGRAM
