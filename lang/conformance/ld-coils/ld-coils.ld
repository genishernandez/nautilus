PROGRAM CoilsLd
VAR_EXTERNAL
    A, B, SetIn, RstIn, CLK : BOOL;
    N : INT;
    ld_Latch, ld_P, ld_N, ld_RiseC, ld_FallC, ld_CntDone, ld_NotZero, ld_Br, ld_Mid : BOOL;
    ld_Cnt : INT;
END_VAR
VAR
    cnt : CTU;
    brt : TON;
    mdt : TON;
END_VAR
LD
  RUNG set
    SetIn ( S ld_Latch )
  RUNG reset
    RstIn ( R ld_Latch )
  RUNG pcoil
    A ( P ld_P )
  RUNG ncoil
    A ( N ld_N )
  RUNG risec
    +B ( ld_RiseC )
  RUNG fallc
    -B ( ld_FallC )
  RUNG count (* FB-only: no coil, outputs read below *)
    CLK cnt:CTU(R := FALSE, PV := 3, CV => ld_Cnt)
  RUNG done
    cnt.Q ( ld_CntDone )
  RUNG notzero
    /EQ(N, 0) ( ld_NotZero )
  RUNG branch
    [ A | [ B | CLK ] brt:TON(PT := T#3ms) ] ( ld_Br )
  RUNG mid (* a block mid-rung: its Q carries power on to B and the coil *)
    A mdt:TON(PT := T#3ms) B ( ld_Mid )
END_LD
END_PROGRAM
