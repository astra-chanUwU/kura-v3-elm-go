module Domain.Selection exposing
    ( Selection
    , addRange
    , clear
    , click
    , count
    , empty
    , extendTo
    , isSelected
    , prune
    , remove
    , selectAll
    , selectRange
    , setActive
    , targets
    , toggle
    )

{-| The active post and the multi-selection are independent: changing the
selection never moves the active post, except when there is no active post yet.
-}

import Domain.Sequence as Sequence exposing (Sequence)
import Set exposing (Set)


type alias Selection =
    { active : Maybe String
    , selected : Set String
    , anchor : Maybe String
    }


empty : Selection
empty =
    { active = Nothing, selected = Set.empty, anchor = Nothing }


{-| Plain click: the post becomes active and the only selected post.
-}
click : String -> Selection -> Selection
click id _ =
    { active = Just id, selected = Set.singleton id, anchor = Just id }


toggle : String -> Selection -> Selection
toggle id selection =
    { selection
        | selected =
            if Set.member id selection.selected then
                Set.remove id selection.selected

            else
                Set.insert id selection.selected
        , anchor = Just id
        , active = orElse (Just id) selection.active
    }


{-| Move the active post without touching the selection.
-}
setActive : String -> Selection -> Selection
setActive id selection =
    { selection | active = Just id, anchor = Just id }


{-| Shift+arrow: the active post moves and the selection becomes the range
from the anchor to it.
-}
extendTo : Sequence -> String -> Selection -> Selection
extendTo sequence id selection =
    let
        origin =
            rangeOrigin id selection
    in
    { active = Just id
    , anchor = Just origin
    , selected = Set.fromList (Sequence.range origin id sequence)
    }


{-| Shift+click: replace the selection with the range from the anchor.
-}
selectRange : Sequence -> String -> Selection -> Selection
selectRange sequence id selection =
    let
        origin =
            rangeOrigin id selection
    in
    { selection
        | anchor = Just origin
        , selected = Set.fromList (Sequence.range origin id sequence)
        , active = orElse (Just id) selection.active
    }


{-| Ctrl/Cmd+Shift+click: add the range from the anchor to the selection.
-}
addRange : Sequence -> String -> Selection -> Selection
addRange sequence id selection =
    let
        origin =
            rangeOrigin id selection
    in
    { selection
        | anchor = Just origin
        , selected = Set.union selection.selected (Set.fromList (Sequence.range origin id sequence))
        , active = orElse (Just id) selection.active
    }


selectAll : Sequence -> Selection -> Selection
selectAll sequence selection =
    { selection | selected = Set.fromList (Sequence.ids sequence) }


clear : Selection -> Selection
clear selection =
    { selection | selected = Set.empty }


remove : String -> Selection -> Selection
remove id selection =
    { selection | selected = Set.remove id selection.selected }


{-| Drop ids that are no longer in the sequence.
-}
prune : Sequence -> Selection -> Selection
prune sequence selection =
    let
        keep =
            Maybe.andThen
                (\id ->
                    if Sequence.member id sequence then
                        Just id

                    else
                        Nothing
                )
    in
    { active = keep selection.active
    , selected = Set.filter (\id -> Sequence.member id sequence) selection.selected
    , anchor = keep selection.anchor
    }


{-| Actions apply to the selection when it is non-empty, otherwise to the
active post.
-}
targets : Sequence -> Selection -> List String
targets sequence selection =
    if Set.isEmpty selection.selected then
        selection.active |> Maybe.map List.singleton |> Maybe.withDefault []

    else
        Sequence.inOrder selection.selected sequence


isSelected : String -> Selection -> Bool
isSelected id selection =
    Set.member id selection.selected


count : Selection -> Int
count selection =
    Set.size selection.selected


rangeOrigin : String -> Selection -> String
rangeOrigin id selection =
    selection.anchor
        |> orElse selection.active
        |> Maybe.withDefault id


orElse : Maybe a -> Maybe a -> Maybe a
orElse fallback value =
    case value of
        Just _ ->
            value

        Nothing ->
            fallback
