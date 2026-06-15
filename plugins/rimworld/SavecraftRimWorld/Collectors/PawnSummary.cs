using RimWorld;
using Verse;

namespace SavecraftRimWorld.Collectors
{
    /// <summary>
    /// Shared accessors for the at-a-glance DLC label scalars surfaced in the colonist
    /// roster. Each returns null when the relevant DLC is inactive or the value is absent,
    /// centralizing the DLC-gating in one place. The colonist *detail* collector reads the
    /// underlying trackers directly because its DLC blocks emit many more fields than the
    /// label alone.
    /// </summary>
    public static class PawnSummary
    {
        public static string Xenotype(Pawn pawn)
        {
            if (!ModsConfig.BiotechActive || pawn.genes == null) return null;
            return pawn.genes.XenotypeLabelCap;
        }

        public static string RoyaltyTitle(Pawn pawn)
        {
            var title = ModsConfig.RoyaltyActive ? pawn.royalty?.MostSeniorTitle : null;
            if (title == null) return null;
            return title.def.GetLabelCapFor(pawn);
        }

        public static string IdeoRole(Pawn pawn)
        {
            var role = ModsConfig.IdeologyActive ? pawn.ideo?.Ideo?.GetRole(pawn) : null;
            if (role == null) return null;
            return role.LabelCap;
        }

        public static string WeaponName(Pawn pawn)
        {
            var weapon = pawn.equipment?.Primary;
            if (weapon == null) return null;
            return weapon.LabelCap;
        }
    }
}
