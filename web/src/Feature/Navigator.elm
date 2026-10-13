module Feature.Navigator exposing (Config, ManageConfig, ManageRow, SavedItem, view)

import Domain.Collection exposing (CollectionSummary)
import Html exposing (Html, aside, button, details, div, form, h3, input, li, p, span, summary, text, ul)
import Html.Attributes exposing (attribute, class, classList, disabled, placeholder, title, type_, value)
import Html.Events exposing (onClick, onInput, onSubmit)
import Set exposing (Set)
import Ui.Icon
import Ui.Panel as Panel exposing (Presentation)


{-| A saved search row. `id` is the server id, or the query itself when
the on-device fallback list is active; `label` is the query to run.
-}
type alias SavedItem =
    { id : String
    , label : String
    }


{-| One visible ordering row. At most one 60-row page is passed in; the
edge flags already account for unfetched following pages.
-}
type alias ManageRow =
    { id : String
    , canUp : Bool
    , canDown : Bool
    , canFirst : Bool
    , canLast : Bool
    }


type alias ManageConfig msg =
    { collectionId : String
    , name : String
    , rows : List ManageRow
    , page : Int
    , pageCount : Int
    , hasPrev : Bool
    , hasNext : Bool
    , loading : Bool
    , busy : Bool
    , onMove : String -> Int -> msg
    , onMoveFirst : String -> msg
    , onMoveLast : String -> msg
    , onRemove : String -> msg
    , onPrev : msg
    , onNext : msg
    , onClose : msg
    }


type alias Config msg =
    { presentation : Presentation
    , current : String
    , saved : List SavedItem
    , savedStatus : Maybe String
    , savedSaving : Bool
    , localFallback : Bool
    , recent : List String
    , collections : List CollectionSummary
    , activeCollection : Maybe String
    , collectionBrowse : Maybe String
    , collectionStatus : Maybe String
    , collectionDraft : String
    , onCollectionDraft : String -> msg
    , onCreateCollection : String -> msg
    , onOpenCollection : String -> msg
    , onAllPosts : msg
    , summariesUnavailable : Maybe String
    , onRetrySummaries : msg
    , manage : Maybe (ManageConfig msg)
    , onManageCollection : String -> msg
    , renamingId : Maybe String
    , renameDraft : String
    , onRenameDraft : String -> msg
    , onStartRename : String -> msg
    , onSubmitRename : msg
    , onCancelRename : msg
    , deleteConfirmId : Maybe String
    , onRequestDelete : String -> msg
    , onConfirmDelete : msg
    , onCancelDelete : msg
    , manageBusy : Bool
    , onRemoveCollectionPost : String -> msg
    , removingPosts : Set String
    , onRun : String -> msg
    , onSave : msg
    , onRemove : String -> msg
    , onClose : msg
    , writesEnabled : Bool
    }


view : Config msg -> Html msg
view config =
    let
        canSave =
            String.trim config.current /= "" && not (List.any (\item -> item.label == config.current) config.saved) && not config.savedSaving

        active =
            config.activeCollection
                |> Maybe.andThen (\id -> List.filter (\c -> c.id == id) config.collections |> List.head)
    in
    aside [ class (Panel.presentationClass "navigator" config.presentation), attribute "aria-label" "Library navigation" ]
        [ Panel.header "Library" config.presentation config.onClose
        , div [ class "panel-body" ]
            [ div [ class "nav-home" ]
                [ button
                    [ class "nav-item nav-library"
                    , classList [ ( "is-current", config.collectionBrowse == Nothing && config.current == "" ) ]
                    , type_ "button"
                    , onClick config.onAllPosts
                    ]
                    [ Ui.Icon.view "folder", span [] [ text "All posts" ] ]
                ]
            , div [ class "panel-section nav-section" ]
                [ div [ class "section-heading" ]
                    [ h3 [ class "panel-section-title" ] [ text "Saved searches" ]
                    , button [ class "nav-add", type_ "button", onClick config.onSave, disabled (not canSave), attribute "aria-label" "Save current search", title "Save current search" ] [ text "+" ]
                    ]
                , ul [ class "nav-list" ]
                    (List.map
                        (\item ->
                            li [ class "nav-row" ]
                                [ queryButton config item.label
                                , button
                                    [ class "nav-remove"
                                    , type_ "button"
                                    , onClick (config.onRemove item.id)
                                    , disabled config.savedSaving
                                    , attribute "aria-label" ("Remove saved search " ++ item.label)
                                    ]
                                    [ text "×" ]
                                ]
                        )
                        config.saved
                    )
                , case config.savedStatus of
                    Just status ->
                        p [ class "panel-note", attribute "role" "status" ] [ text status ]

                    Nothing ->
                        text ""
                , if config.localFallback && not (List.isEmpty config.saved) then
                    p [ class "panel-note" ] [ text "On this device" ]

                  else
                    text ""
                ]
            , if List.isEmpty config.recent then
                text ""

              else
                details [ class "panel-section nav-disclosure" ]
                    [ summary [] [ Ui.Icon.view "search", text "Recent searches" ]
                    , ul [ class "nav-list" ] (List.map (\query -> li [ class "nav-row" ] [ queryButton config query ]) config.recent)
                    ]
            , div [ class "panel-section nav-section" ]
                [ h3 [ class "panel-section-title" ] [ text "Collections" ]
                , case config.summariesUnavailable of
                    Just message ->
                        div []
                            [ p [ class "panel-note status-error" ] [ text ("Collections are unavailable: " ++ message) ]
                            , button [ class "button button-quiet", type_ "button", onClick config.onRetrySummaries ] [ text "Retry" ]
                            ]

                    Nothing ->
                        text ""
                , ul [ class "nav-list" ]
                    (List.map
                        (\collection ->
                            li [ class "nav-row" ]
                                [ button
                                    [ class "nav-item nav-collection"
                                    , classList [ ( "is-current", Just collection.id == config.collectionBrowse ) ]
                                    , type_ "button"
                                    , onClick (config.onOpenCollection collection.id)
                                    ]
                                    [ Ui.Icon.view "folder"
                                    , span [ class "nav-name" ] [ text collection.name ]
                                    , span
                                        [ class "nav-count"
                                        , title (String.fromInt collection.memberCount ++ " members")
                                        ]
                                        [ text (String.fromInt collection.visiblePostCount) ]
                                    ]
                                ]
                        )
                        config.collections
                    )
                , details [ class "nav-create" ]
                    [ summary [ title "Create a collection" ] [ text "+ New collection" ]
                    , form [ class "compact-form", onSubmit (config.onCreateCollection config.collectionDraft) ]
                        [ input [ class "nav-input", type_ "text", placeholder "Collection name", value config.collectionDraft, onInput config.onCollectionDraft, attribute "aria-label" "New collection name", disabled (not config.writesEnabled) ] []
                        , button [ class "button", type_ "submit", disabled (String.trim config.collectionDraft == "" || not config.writesEnabled) ] [ text "Create" ]
                        ]
                    ]
                , case config.collectionStatus of
                    Just status ->
                        p [ class "panel-note", attribute "role" "status" ] [ text status ]

                    Nothing ->
                        text ""
                , case active of
                    Nothing ->
                        text ""

                    Just collection ->
                        div []
                            [ manageControls config collection
                            , renameControls config collection
                            , deleteControls config collection
                            , case config.manage of
                                Nothing ->
                                    text ""

                                Just manage ->
                                    if manage.collectionId == collection.id then
                                        manageSection config manage

                                    else
                                        text ""
                            ]
                ]
            ]
        ]


manageControls : Config msg -> CollectionSummary -> Html msg
manageControls config collection =
    case config.manage of
        Just manage ->
            if manage.collectionId == collection.id then
                text ""

            else
                openManageButton config collection

        Nothing ->
            openManageButton config collection


openManageButton : Config msg -> CollectionSummary -> Html msg
openManageButton config collection =
    if collection.visiblePostCount == 0 then
        text ""

    else
        p []
            [ button
                [ class "button button-quiet"
                , type_ "button"
                , onClick (config.onManageCollection collection.id)
                ]
                [ text ("Manage order in " ++ collection.name) ]
            ]


renameControls : Config msg -> CollectionSummary -> Html msg
renameControls config collection =
    if config.renamingId == Just collection.id then
        form [ class "compact-form", onSubmit config.onSubmitRename ]
            [ input
                [ class "nav-input"
                , type_ "text"
                , placeholder "Collection name"
                , value config.renameDraft
                , onInput config.onRenameDraft
                , attribute "aria-label" "Rename collection"
                , disabled (not config.writesEnabled || config.manageBusy)
                ]
                []
            , button [ class "button", type_ "submit", disabled (String.trim config.renameDraft == "" || not config.writesEnabled || config.manageBusy) ] [ text "Save name" ]
            , button [ class "button button-quiet", type_ "button", onClick config.onCancelRename ] [ text "Cancel" ]
            ]

    else
        p []
            [ button
                [ class "button button-quiet"
                , type_ "button"
                , onClick (config.onStartRename collection.id)
                , disabled (not config.writesEnabled || config.manageBusy)
                , attribute "aria-label" ("Rename " ++ collection.name)
                ]
                [ text "Rename" ]
            ]


deleteControls : Config msg -> CollectionSummary -> Html msg
deleteControls config collection =
    if config.deleteConfirmId == Just collection.id then
        div []
            [ p [ class "panel-note status-error" ]
                [ text ("Delete “" ++ collection.name ++ "”? This deletes the collection and its memberships only; its posts stay in the library.") ]
            , p []
                [ button
                    [ class "button"
                    , type_ "button"
                    , onClick config.onConfirmDelete
                    , disabled (not config.writesEnabled || config.manageBusy)
                    ]
                    [ text "Delete collection" ]
                , button [ class "button button-quiet", type_ "button", onClick config.onCancelDelete ] [ text "Cancel" ]
                ]
            ]

    else
        p []
            [ button
                [ class "button button-quiet"
                , type_ "button"
                , onClick (config.onRequestDelete collection.id)
                , disabled (not config.writesEnabled || config.manageBusy)
                , attribute "aria-label" ("Delete " ++ collection.name)
                ]
                [ text "Delete" ]
            ]


manageSection : Config msg -> ManageConfig msg -> Html msg
manageSection config manage =
    div [ class "collection-order" ]
        [ p [ class "panel-note" ]
            [ text
                (if manage.loading then
                    "Loading ordering…"

                 else
                    "Ordering in " ++ manage.name ++ " (page " ++ String.fromInt (manage.page + 1) ++ " of " ++ String.fromInt manage.pageCount ++ ")"
                )
            ]
        , ul [ class "nav-list collection-order-list" ]
            (List.map
                (\row ->
                    li [ class "nav-row collection-order-row" ]
                        [ span [ class "collection-order-id", title row.id ] [ text ("#" ++ row.id) ]
                        , button [ class "collection-move", type_ "button", disabled (not row.canFirst || not config.writesEnabled || manage.busy), onClick (manage.onMoveFirst row.id), attribute "aria-label" ("Move #" ++ row.id ++ " to first") ] [ text "⇤" ]
                        , button [ class "collection-move", type_ "button", disabled (not row.canUp || not config.writesEnabled || manage.busy), onClick (manage.onMove row.id -1), attribute "aria-label" ("Move #" ++ row.id ++ " up") ] [ text "↑" ]
                        , button [ class "collection-move", type_ "button", disabled (not row.canDown || not config.writesEnabled || manage.busy), onClick (manage.onMove row.id 1), attribute "aria-label" ("Move #" ++ row.id ++ " down") ] [ text "↓" ]
                        , button [ class "collection-move", type_ "button", disabled (not row.canLast || not config.writesEnabled || manage.busy), onClick (manage.onMoveLast row.id), attribute "aria-label" ("Move #" ++ row.id ++ " to last") ] [ text "⇥" ]
                        , button [ class "collection-remove", type_ "button", disabled (not config.writesEnabled || manage.busy || Set.member row.id config.removingPosts), onClick (manage.onRemove row.id), attribute "aria-label" ("Remove #" ++ row.id ++ " from collection") ] [ text "×" ]
                        ]
                )
                manage.rows
            )
        , p []
            [ button [ class "button button-quiet", type_ "button", disabled (not manage.hasPrev || manage.busy), onClick manage.onPrev ] [ text "Back" ]
            , button [ class "button button-quiet", type_ "button", disabled (not manage.hasNext || manage.busy), onClick manage.onNext ] [ text "Next" ]
            , button [ class "button button-quiet", type_ "button", onClick manage.onClose ] [ text "Close" ]
            ]
        ]


queryButton : Config msg -> String -> Html msg
queryButton config query =
    button
        [ class "nav-item"
        , classList [ ( "is-current", query == config.current ) ]
        , type_ "button"
        , title query
        , onClick (config.onRun query)
        ]
        [ Ui.Icon.view "search", span [ class "nav-name" ] [ text query ] ]
